import { memo, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { Color, usePoll, useTick } from './transit.ts'

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

type Board = {
  Tracks: Track[]
  Stations: Station[]
  Trains: Train[]
}

// Basemap is made by backend/basemap from OpenStreetMap: per layer a list
// of features, each a list of lines or rings, each lon, lat, lon, lat, ...
type Basemap = {
  Layers: Record<string, number[][][]>
}

const METERS_PER_DEGREE = 111_320
// When new data moves a train, it glides there: the difference fades with
// a time constant of GLIDE ms, or longer for big differences so that it
// doesn't go faster than GLIDE_SPEED m/s. Even bigger moves are made at
// once.
const GLIDE = 1_800
const GLIDE_SPEED = 100
const MAX_GLIDE = 4_000 // m

type Point = [number, number]

// View is the part of the map shown: width meters across, around center,
// turned rotate degrees counterclockwise so that the tracks run along the
// box.
export type View = {
  center: [number, number] // lon, lat
  width: number
  rotate: number
}

// Projection maps lon, lat to pixels in a box of w x h, on a flat
// approximation that is fine at city scale. scale is pixels per meter.
type Projection = {
  project: (lon: number, lat: number) => Point
  scale: number
  w: number
  h: number
}

function projection(view: View, w: number, h: number): Projection {
  const [lon0, lat0] = view.center
  const k = Math.cos(lat0 * Math.PI / 180) * METERS_PER_DEGREE
  const scale = w / view.width
  const cos = Math.cos(view.rotate * Math.PI / 180), sin = Math.sin(view.rotate * Math.PI / 180)
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

// Basemap draws streets, parks and water. It never changes, so it is only
// drawn once.
const BasemapLayers = memo(function BasemapLayers({ basemap, proj }: { basemap: Basemap, proj: Projection }) {
  const layer = (name: string) => basemap.Layers[name] ?? []
  const lines = (name: string) => layer(name).flatMap(f => f.map(l => line(points(l, proj)))).join('')
  return (
    <g>
      {['water', 'green'].flatMap(name => layer(name).map((f, i) => (
        <path key={`${name}${i}`} className={`trainmap-${name}`} d={f.map(r => line(points(r, proj)) + 'Z').join('')} />
      )))}
      {['minor', 'medium', 'major'].map(name => (
        <path key={name} className={`trainmap-road trainmap-${name}`} d={lines(name)} />
      ))}
    </g>
  )
})

function Tracks({ tracks, stations, proj }: { tracks: Track[], stations: Station[], proj: Projection }) {
  return (
    <g>
      {tracks.map(t => (
        <path key={t.Kind} className={`trainmap-track ${t.Kind}`}
          d={t.Segments.map(s => line([proj.project(s[0], s[1]), proj.project(s[2], s[3])])).join('')} />
      ))}
      {stations.map(s => {
        const [x, y] = proj.project(s.Lon, s.Lat)
        return (
          <g key={s.Kind + s.Name} className={`trainmap-station ${s.Kind}${s.Home ? ' home' : ''}`}>
            <circle cx={x} cy={y} />
            {s.Kind === 'lirr' && <text x={x} y={y}>{s.Name}</text>}
          </g>
        )
      })}
    </g>
  )
}

// A path measured in meters, for placing trains along it.
type Measured = { pts: Point[], dist: number[] }

function measure(pts: Point[], scale: number): Measured {
  const dist = [0]
  for (let i = 1; i < pts.length; i++) {
    dist.push(dist[i - 1] + Math.hypot(pts[i][0] - pts[i - 1][0], pts[i][1] - pts[i - 1][1]) / scale)
  }
  return { pts, dist }
}

// stretch returns the part of path from a to b meters along it.
function stretch({ pts, dist }: Measured, a: number, b: number): Point[] {
  const at = (d: number): Point => {
    let i = 1
    while (i < pts.length - 1 && dist[i] < d) i++
    const t = Math.max(0, Math.min(1, (d - dist[i - 1]) / (dist[i] - dist[i - 1] || 1)))
    return [pts[i - 1][0] + t * (pts[i][0] - pts[i - 1][0]), pts[i - 1][1] + t * (pts[i][1] - pts[i - 1][1])]
  }
  const out = [at(a)]
  for (let i = 0; i < pts.length; i++) {
    if (dist[i] > a && dist[i] < b) out.push(pts[i])
  }
  out.push(at(b))
  return out
}

type Motion = { knots: { t: number, along: number }[], ease: boolean }

function motion(t: Train): Motion {
  return { knots: t.Knots.map(k => ({ t: new Date(k.Time).getTime(), along: k.Along })), ease: t.Ease }
}

// position is where the middle of a train is at now, by its own data. The
// backend's positionAt does the same.
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

// Trains draws the trains car by car, moving them a little every tick.
// When new data moves a train, it glides there rather than jumps.
function Trains({ trains, proj }: { trains: Train[], proj: Projection }) {
  const now = useTick(100).getTime()
  const paths = useMemo(() => new Map(trains.map(t => [t.ID, measure(points(t.Path, proj), proj.scale)])), [trains, proj])
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
        const path = paths.get(t.ID)!
        const glide = glides.offsets.get(t.ID)
        const offset = glide ? glide.d * Math.exp(-(now - glides.t0) / glide.tau) : 0
        const along = position(motions.get(t.ID)!, now) + offset
        shown.current.set(t.ID, { along, path: t.Path.slice(0, 2).join() })

        // Cars are told apart by a pixel's gap, if they are long enough
        // for that to show.
        const length = t.Cars * t.CarLength
        const gap = t.CarLength * proj.scale >= 5 ? 1 / proj.scale : 0
        const cars = []
        for (let i = 0; i < t.Cars; i++) {
          const front = along + length / 2 - i * t.CarLength
          cars.push(line(stretch(path, front - t.CarLength + gap / 2, front - gap / 2)))
        }
        const [hx, hy] = stretch(path, along + length / 2, along + length / 2)[0]
        return (
          <g key={t.ID} className={`trainmap-train ${t.Kind}`} style={{ color: t.Color }}>
            <path d={cars.join('')} />
            <circle cx={hx} cy={hy} />
          </g>
        )
      })}
    </g>
  )
}

// TrainMap is a map of the tracks around the station with the trains on
// their way to it, as src (see trainMap in the backend) has them, over
// basemap. It shows view, which should be within the area the backend
// covers.
function TrainMap({ src, basemap, view }: { src: string, basemap: string, view: View }) {
  const board = usePoll<Board>(src, 5_000)
  const [map, setMap] = useState<Basemap>()
  useEffect(() => {
    fetch(basemap).then(r => r.json()).then(setMap).catch(e => console.error(e))
  }, [basemap])

  const ref = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState<[number, number]>()
  useLayoutEffect(() => {
    const el = ref.current!
    const update = () => setSize([el.clientWidth, el.clientHeight])
    update()
    const obs = new ResizeObserver(update)
    obs.observe(el)
    return () => obs.disconnect()
  }, [])

  const proj = useMemo(() => size && projection(view, size[0], size[1]), [view, size])

  return (
    <div className="trainmap" ref={ref}>
      {board && proj && (
        <svg width={proj.w} height={proj.h}>
          {map && <BasemapLayers basemap={map} proj={proj} />}
          <Tracks tracks={board.Tracks} stations={board.Stations} proj={proj} />
          <Trains trains={board.Trains} proj={proj} />
        </svg>
      )}
      <div className="trainmap-credit">© OpenStreetMap</div>
    </div>
  )
}

export default TrainMap
