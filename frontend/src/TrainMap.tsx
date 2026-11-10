import { memo, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { Color, usePoll, useTick } from './transit.ts'

// What /api/map and /api/map/trains return; see trainMap in the backend.

type MapData = {
  View: View
  Basemap: Record<string, number[][][]> | null // lines or rings of lon, lat, ...
  Tracks: Track[]
  Stations: Station[]
  Complete: boolean
}

// View is the part of the map shown: Width x Height meters around Center,
// turned Rotate degrees counterclockwise.
type View = {
  Center: [number, number] // lon, lat
  Width: number
  Height: number
  Rotate: number
}

type Track = {
  Kind: 'lirr' | 'subway'
  Segments: number[][] // [lon, lat, lon, lat]
}

type Station = {
  Name: string
  Kind: 'lirr' | 'subway'
  Lon: number
  Lat: number
  Home: boolean
}

// Knot is where a train is at Time, in meters along its path.
type Knot = {
  Time: string
  Along: number
}

// Train is a train on Path (lon, lat, lon, lat, ...). Its middle moves from
// knot to knot: steadily, or with Ease like a train between stations,
// speeding up and then slowing down.
type Train = Color & {
  ID: string
  Kind: 'lirr' | 'subway'
  Label: string
  Cars: number
  CarLength: number
  Path: number[]
  Knots: Knot[]
  Ease: boolean
}

const METERS_PER_DEGREE = 111_320
// Trains are drawn at a fixed scale, whatever the map's: this many pixels
// per meter of train, and this wide.
const TRAIN_SCALE = 0.15
const TRAIN_WIDTH = 5
// When new data moves a train, it glides there: the difference fades with
// a time constant of GLIDE ms, or longer for big differences so that it
// doesn't go faster than GLIDE_SPEED m/s. Even bigger moves are made at
// once.
const GLIDE = 1_800
const GLIDE_SPEED = 100
const MAX_GLIDE = 4_000 // m

type Point = [number, number]

// Projection maps lon, lat to pixels in a box of w x h that fits the
// view's width, on a flat approximation that is fine at city scale. scale
// is pixels per meter.
type Projection = {
  project: (lon: number, lat: number) => Point
  scale: number
  w: number
  h: number
}

function projection(view: View, w: number, h: number): Projection {
  const [lon0, lat0] = view.Center
  const k = Math.cos(lat0 * Math.PI / 180) * METERS_PER_DEGREE
  const scale = w / view.Width
  const cos = Math.cos(view.Rotate * Math.PI / 180), sin = Math.sin(view.Rotate * Math.PI / 180)
  return {
    project: (lon, lat) => {
      const x = (lon - lon0) * k, y = (lat - lat0) * METERS_PER_DEGREE
      return [w / 2 + (x * cos - y * sin) * scale, h / 2 - (x * sin + y * cos) * scale]
    },
    scale, w, h,
  }
}

function points(flat: number[], proj: Projection): Point[] {
  const pts: Point[] = []
  for (let i = 0; i + 1 < flat.length; i += 2) {
    pts.push(proj.project(flat[i], flat[i + 1]))
  }
  return pts
}

function line(pts: Point[]): string {
  return pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`).join('')
}

// Basemap draws streets, parks and water. It only changes with the map.
const Basemap = memo(function Basemap({ layers, proj }: { layers: Record<string, number[][][]>, proj: Projection }) {
  const layer = (name: string) => layers[name] ?? []
  // Areas with holes need the even-odd rule each; the rest can be one path.
  const areas = (name: string) => {
    const simple = layer(name).filter(f => f.length === 1)
    const holed = layer(name).filter(f => f.length > 1)
    const d = (rings: number[][]) => rings.map(r => line(points(r, proj)) + 'Z').join('')
    return [
      <path key={name} className={`trainmap-${name}`} d={d(simple.flat())} />,
      ...holed.map((f, i) => <path key={name + i} className={`trainmap-${name}`} fillRule="evenodd" d={d(f)} />),
    ]
  }
  const lines = (name: string) => layer(name).flatMap(f => f.map(l => line(points(l, proj)))).join('')
  return (
    <g>
      {areas('water')}
      {areas('green')}
      {['minor', 'medium', 'major'].map(name => (
        <path key={name} className={`trainmap-road trainmap-${name}`} d={lines(name)} />
      ))}
    </g>
  )
})

// label is how a station is named on the map: long subway names are cut
// to their first part, unless another station already has that name.
function labels(stations: Station[]): string[] {
  const names = new Set(stations.map(s => s.Name))
  return stations.map(s => {
    const parts = s.Name.split('-')
    const short = parts[0]
    if (s.Name.length <= 20 || parts.length === 1) {
      return s.Name
    }
    return names.has(short) || stations.some(o => o !== s && o.Name.startsWith(short + '-'))
      ? parts.slice(0, 2).join('-')
      : short
  })
}

type Box = { x0: number, y0: number, x1: number, y1: number }

const overlaps = (a: Box, b: Box) => a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1

// placeLabels puts each station's label beside it where it doesn't cover
// another label or station, trying the home stations first, then the
// LIRR's. Labels that don't fit anywhere are left out.
function placeLabels(stations: Station[], proj: Projection, fontSize: number) {
  const names = labels(stations)
  const at = stations.map(s => proj.project(s.Lon, s.Lat))
  const r = 4
  const taken: Box[] = at.map(([x, y]) => ({ x0: x - r, y0: y - r, x1: x + r, y1: y + r }))
  const order = stations.map((_, i) => i).sort((a, b) =>
    Number(stations[b].Home) - Number(stations[a].Home) ||
    Number(stations[b].Kind === 'lirr') - Number(stations[a].Kind === 'lirr'))

  const placed: { i: number, x: number, y: number, anchor: 'start' | 'end' }[] = []
  for (const i of order) {
    const [x, y] = at[i]
    const w = names[i].length * fontSize * 0.55, h = fontSize
    const gap = r + 2
    const candidates: [number, number, 'start' | 'end'][] = [
      [x + gap, y - gap, 'start'], [x + gap, y + gap + h, 'start'],
      [x - gap, y - gap, 'end'], [x - gap, y + gap + h, 'end'],
    ]
    for (const [tx, ty, anchor] of candidates) {
      const x0 = anchor === 'start' ? tx : tx - w
      const box = { x0, y0: ty - h, x1: x0 + w, y1: ty }
      if (box.x0 < 0 || box.x1 > proj.w || !taken.every(b => !overlaps(b, box))) {
        continue
      }
      taken.push(box)
      placed.push({ i, x: tx, y: ty, anchor })
      break
    }
  }
  return placed.map(p => ({ ...p, name: names[p.i], station: stations[p.i] }))
}

function Tracks({ tracks, stations, proj, fontSize }: { tracks: Track[], stations: Station[], proj: Projection, fontSize: number }) {
  const placed = useMemo(() => placeLabels(stations, proj, fontSize), [stations, proj, fontSize])
  return (
    <g>
      {tracks.map(t => (
        <path key={t.Kind} className={`trainmap-track ${t.Kind}`}
          d={t.Segments.map(s => line([proj.project(s[0], s[1]), proj.project(s[2], s[3])])).join('')} />
      ))}
      {stations.map(s => {
        const [x, y] = proj.project(s.Lon, s.Lat)
        return <circle key={s.Kind + s.Name} className={`trainmap-station ${s.Kind}${s.Home ? ' home' : ''}`} cx={x} cy={y} />
      })}
      {placed.map(p => (
        <text key={p.station.Kind + p.station.Name} x={p.x} y={p.y} textAnchor={p.anchor}
          className={`trainmap-label ${p.station.Kind}${p.station.Home ? ' home' : ''}`}>{p.name}</text>
      ))}
    </g>
  )
}

// A path on screen, with the distance to each point.
type Measured = { pts: Point[], dist: number[] }

function measure(pts: Point[]): Measured {
  const dist = [0]
  for (let i = 1; i < pts.length; i++) {
    dist.push(dist[i - 1] + Math.hypot(pts[i][0] - pts[i - 1][0], pts[i][1] - pts[i - 1][1]))
  }
  return { pts, dist }
}

const length = ({ dist }: Measured) => dist[dist.length - 1]

// simplify drops the points of pts that are within tolerance of the line
// through their neighbors (Douglas-Peucker), so that small wiggles in a
// track don't make trains on it look shorter.
function simplify(pts: Point[], tolerance: number): Point[] {
  if (pts.length < 3) {
    return pts
  }
  const keep = new Array(pts.length).fill(false)
  keep[0] = keep[pts.length - 1] = true
  const rec = (a: number, b: number) => {
    const [ax, ay] = pts[a], [bx, by] = pts[b]
    const len = Math.hypot(bx - ax, by - ay) || 1
    let far = 0, idx = -1
    for (let i = a + 1; i < b; i++) {
      const d = Math.abs((bx - ax) * (ay - pts[i][1]) - (ax - pts[i][0]) * (by - ay)) / len
      if (d > far) {
        far = d
        idx = i
      }
    }
    if (far > tolerance) {
      keep[idx] = true
      rec(a, idx)
      rec(idx, b)
    }
  }
  rec(0, pts.length - 1)
  return pts.filter((_, i) => keep[i])
}

// pointAt is the point d pixels along path, carrying on straight past its
// ends.
function pointAt({ pts, dist }: Measured, d: number): Point {
  let i = 1
  while (i < pts.length - 1 && dist[i] < d) i++
  const t = (d - dist[i - 1]) / (dist[i] - dist[i - 1] || 1)
  return [pts[i - 1][0] + t * (pts[i][0] - pts[i - 1][0]), pts[i - 1][1] + t * (pts[i][1] - pts[i - 1][1])]
}

// trainShape is the outline of a train from tail to front, pixels along
// path: a band along the track, pointed at the front.
function trainShape(path: Measured, tail: number, front: number): string {
  const spine = [pointAt(path, tail)]
  path.dist.forEach((d, i) => d > tail && d < front && spine.push(path.pts[i]))
  spine.push(pointAt(path, front))

  const w = TRAIN_WIDTH / 2
  const left: Point[] = [], right: Point[] = []
  spine.forEach(([x, y], i) => {
    const [ax, ay] = spine[Math.max(0, i - 1)], [bx, by] = spine[Math.min(spine.length - 1, i + 1)]
    const len = Math.hypot(bx - ax, by - ay) || 1
    const nx = -(by - ay) / len * w, ny = (bx - ax) / len * w
    left.push([x + nx, y + ny])
    right.push([x - nx, y - ny])
  })
  const [fx, fy] = spine[spine.length - 1], [px, py] = spine[spine.length - 2]
  const len = Math.hypot(fx - px, fy - py) || 1
  const tip: Point = [fx + (fx - px) / len * w * 2, fy + (fy - py) / len * w * 2]
  return line([...left, tip, ...right.reverse()]) + 'Z'
}

type Motion = { knots: { t: number, along: number }[], ease: boolean }

function motion(t: Train): Motion {
  return { knots: t.Knots.map(k => ({ t: new Date(k.Time).getTime(), along: k.Along })), ease: t.Ease }
}

// position is where the middle of a train is at now, in meters along its
// path. The backend's transit.PositionAt does the same.
function position({ knots, ease }: Motion, now: number): number {
  if (knots.length === 0) {
    return 0
  }
  for (let i = 1; i < knots.length; i++) {
    const a = knots[i - 1], b = knots[i]
    if (now < b.t) {
      if (now < a.t) {
        return a.along
      }
      let f = (now - a.t) / (b.t - a.t)
      if (ease) {
        f = f * f * (3 - 2 * f)
      }
      return a.along + (b.along - a.along) * f
    }
  }
  return knots[knots.length - 1].along
}

// Trains draws the trains, moving them a little every tick. When new data
// moves a train, it glides there rather than jumps.
function Trains({ trains, proj }: { trains: Train[], proj: Projection }) {
  const now = useTick(100).getTime()
  const paths = useMemo(() => new Map(trains.map(t => {
    const raw = points(t.Path, proj)
    // Positions are meters along the path as the backend measures it.
    const meters = length(measure(raw)) / proj.scale
    const path = measure(simplify(raw, 1))
    return [t.ID, { path, perMeter: length(path) / (meters || 1) }]
  })), [trains, proj])
  const motions = useMemo(() => new Map(trains.map(t => [t.ID, motion(t)])), [trains])

  // Where each train was last drawn, and how far that was off from where
  // the newest data has it, when that data came.
  const shown = useRef(new Map<string, { along: number, path: string }>())
  const glides = useMemo(() => {
    const g = new Map<string, { d: number, tau: number }>()
    const t0 = Date.now()
    for (const t of trains) {
      const prev = shown.current.get(t.ID)
      const d = prev ? prev.along - position(motions.get(t.ID)!, t0) : 0
      if (prev && prev.path === t.Path.slice(0, 2).join() && Math.abs(d) < MAX_GLIDE) {
        g.set(t.ID, { d, tau: Math.max(GLIDE, Math.abs(d) / GLIDE_SPEED * 1000) })
      }
    }
    return { t0, offsets: g }
  }, [trains, motions])

  return (
    <g>
      {trains.map(t => {
        const glide = glides.offsets.get(t.ID)
        const offset = glide ? glide.d * Math.exp(-(now - glides.t0) / glide.tau) : 0
        const along = position(motions.get(t.ID)!, now) + offset
        shown.current.set(t.ID, { along, path: t.Path.slice(0, 2).join() })

        const { path, perMeter } = paths.get(t.ID)!
        const middle = along * perMeter
        const length = t.Cars * t.CarLength * TRAIN_SCALE
        return (
          <path key={t.ID} className={`trainmap-train ${t.Kind}`} style={{ fill: t.Color }}
            d={trainShape(path, middle - length / 2, middle + length / 2)} />
        )
      })}
    </g>
  )
}

// useSize returns the size of the element ref is on, in pixels.
function useSize(ref: React.RefObject<HTMLElement>): [number, number] | undefined {
  const [size, setSize] = useState<[number, number]>()
  useLayoutEffect(() => {
    const el = ref.current!
    const update = () => setSize([el.clientWidth, el.clientHeight])
    update()
    const obs = new ResizeObserver(update)
    obs.observe(el)
    return () => obs.disconnect()
  }, [ref])
  return size
}

// TrainMap is a map of the tracks around the station with the trains on
// their way to it, as the backend has them at src (the map) and
// src/trains.
function TrainMap({ src }: { src: string }) {
  const [complete, setComplete] = useState(false)
  // The map changes daily; until it is all there, ask again soon.
  const map = usePoll<MapData>(src, complete ? 3_600_000 : 30_000)
  const trains = usePoll<{ Trains: Train[] }>(src + '/trains', 5_000)
  if (map && map.Complete !== complete) {
    setComplete(map.Complete)
  }

  const ref = useRef<HTMLDivElement>(null)
  const size = useSize(ref)
  const view = map?.View
  const proj = useMemo(() => view && size && projection(view, size[0], size[1]), [view, size])
  const fontSize = size ? size[1] * 0.045 : 0

  return (
    <div className="trainmap" ref={ref}>
      {map && proj && (
        <svg width={proj.w} height={proj.h} style={{ fontSize }}>
          {map.Basemap && <Basemap layers={map.Basemap} proj={proj} />}
          <Tracks tracks={map.Tracks} stations={map.Stations} proj={proj} fontSize={fontSize} />
          {trains && <Trains trains={trains.Trains} proj={proj} />}
        </svg>
      )}
      <div className="trainmap-credit">© OpenStreetMap</div>
    </div>
  )
}

export default TrainMap
