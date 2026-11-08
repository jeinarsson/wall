import { FC, useEffect, useRef } from 'react'

// Contours draws faint topographic lines behind the clock. The height field is a
// few broad bumps that wander on slow orbits, so the pattern changes over hours
// but never visibly moves. It is redrawn whenever `time` changes (once a minute).

interface Bump {
    x: number; y: number;      // rest position, fraction of width/height
    orbit: number;             // orbit radius, fraction of width
    period: number;            // orbit period, minutes
    phase: number;
    sigma: number;             // spread, fraction of width
    height: number;
}

// Placed towards the edges so the middle, where the digits are, stays calm.
const BUMPS: Bump[] = [
    { x: 0.04, y: 0.30, orbit: 0.05, period: 9 * 60, phase: 0.0, sigma: 0.20, height: 1.0 },
    { x: 0.98, y: 0.95, orbit: 0.06, period: 13 * 60, phase: 2.1, sigma: 0.24, height: 0.9 },
    { x: 0.80, y: 0.02, orbit: 0.04, period: 7 * 60, phase: 4.0, sigma: 0.16, height: -0.7 },
    { x: 0.22, y: 1.05, orbit: 0.05, period: 11 * 60, phase: 1.3, sigma: 0.18, height: -0.6 },
    { x: 0.50, y: 0.50, orbit: 0.08, period: 17 * 60, phase: 5.2, sigma: 0.45, height: 0.35 },
]

const STEP = 8           // grid spacing in CSS px
const LEVELS = 16
const LINE_COLOR = 'rgba(216, 212, 204, 0.075)'

function field(w: number, h: number, minutes: number) {
    const cols = Math.ceil(w / STEP) + 1
    const rows = Math.ceil(h / STEP) + 1
    const values = new Float32Array(cols * rows)
    const bumps = BUMPS.map(b => {
        const a = b.phase + 2 * Math.PI * minutes / b.period
        return {
            x: (b.x + b.orbit * Math.cos(a)) * w,
            y: b.y * h + b.orbit * w * Math.sin(a),
            k: 1 / (2 * (b.sigma * w) ** 2),
            height: b.height,
        }
    })
    // A gentle warp keeps the rings from looking like perfect ellipses.
    const warp = 0.03 * w
    const wa = minutes / (23 * 60)
    for (let j = 0; j < rows; j++) {
        for (let i = 0; i < cols; i++) {
            const x0 = i * STEP, y0 = j * STEP
            const x = x0 + warp * Math.sin(y0 / h * 5.1 + wa)
            const y = y0 + warp * Math.sin(x0 / w * 4.3 - wa)
            let v = 0
            for (const b of bumps) {
                v += b.height * Math.exp(-((x - b.x) ** 2 + (y - b.y) ** 2) * b.k)
            }
            values[j * cols + i] = v
        }
    }
    return { values, cols, rows }
}

// Marching squares: append one line segment per cell crossing `level`.
function traceLevel(ctx: CanvasRenderingContext2D, values: Float32Array, cols: number, rows: number, level: number) {
    const lerp = (a: number, b: number) => (level - a) / (b - a)
    for (let j = 0; j < rows - 1; j++) {
        for (let i = 0; i < cols - 1; i++) {
            const tl = values[j * cols + i], tr = values[j * cols + i + 1]
            const bl = values[(j + 1) * cols + i], br = values[(j + 1) * cols + i + 1]
            const idx = (tl > level ? 8 : 0) | (tr > level ? 4 : 0) | (br > level ? 2 : 0) | (bl > level ? 1 : 0)
            if (idx === 0 || idx === 15) continue

            const x = i * STEP, y = j * STEP
            const top: [number, number] = [x + STEP * lerp(tl, tr), y]
            const right: [number, number] = [x + STEP, y + STEP * lerp(tr, br)]
            const bottom: [number, number] = [x + STEP * lerp(bl, br), y + STEP]
            const left: [number, number] = [x, y + STEP * lerp(tl, bl)]
            const seg = (a: [number, number], b: [number, number]) => {
                ctx.moveTo(a[0], a[1])
                ctx.lineTo(b[0], b[1])
            }
            switch (idx) {
                case 1: case 14: seg(left, bottom); break
                case 2: case 13: seg(bottom, right); break
                case 3: case 12: seg(left, right); break
                case 4: case 11: seg(top, right); break
                case 6: case 9: seg(top, bottom); break
                case 7: case 8: seg(left, top); break
                case 5: seg(left, top); seg(bottom, right); break
                case 10: seg(top, right); seg(left, bottom); break
            }
        }
    }
}

function draw(canvas: HTMLCanvasElement, time: Date) {
    const w = canvas.clientWidth, h = canvas.clientHeight
    const dpr = window.devicePixelRatio || 1
    canvas.width = w * dpr
    canvas.height = h * dpr
    const ctx = canvas.getContext('2d')!
    ctx.scale(dpr, dpr)

    const minutes = time.getTime() / 60000
    const { values, cols, rows } = field(w, h, minutes)
    let min = Infinity, max = -Infinity
    for (const v of values) {
        min = Math.min(min, v)
        max = Math.max(max, v)
    }

    ctx.beginPath()
    for (let l = 1; l <= LEVELS; l++) {
        traceLevel(ctx, values, cols, rows, min + (max - min) * l / (LEVELS + 1))
    }
    ctx.strokeStyle = LINE_COLOR
    ctx.lineWidth = 1.5
    ctx.lineCap = 'round'
    ctx.stroke()

    // Fade the lines out towards the centre so the clock sits on clean black.
    ctx.globalCompositeOperation = 'destination-out'
    ctx.translate(w / 2, h / 2)
    ctx.scale(1, h / w)
    const r = w * 0.42
    const fade = ctx.createRadialGradient(0, 0, 0, 0, 0, r)
    fade.addColorStop(0, 'rgba(0,0,0,1)')
    fade.addColorStop(0.55, 'rgba(0,0,0,0.85)')
    fade.addColorStop(1, 'rgba(0,0,0,0)')
    ctx.fillStyle = fade
    ctx.fillRect(-r, -r, 2 * r, 2 * r)
}

const Contours: FC<{ time: Date }> = ({ time }) => {
    const canvasRef = useRef<HTMLCanvasElement>(null)

    useEffect(() => {
        if (canvasRef.current) draw(canvasRef.current, time)
    }, [time])

    return <canvas className="contours" ref={canvasRef} />
}

export default Contours
