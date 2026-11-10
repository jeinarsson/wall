import { format } from 'date-fns/format'

import { Alerts, Heading, Minutes } from './Sign.tsx'
import { Alert, minutesUntil, usePoll, useTick } from './transit.ts'

type Departure = {
  Train: string
  Destination: string
  Color: string
  Scheduled: string
  Expected: string | null // null without a live prediction
  Canceled: boolean
  Status: string // "Arriving", "At station", "Held" or ""
}

type Board = {
  Departures: Departure[]
  Alerts: Alert[]
}

// Delays from which they are shown, and turn red.
const LATE = 60_000
const VERY_LATE = 5 * 60_000

// Note says what is special about a train, if anything: canceled, its
// status at the station, or else how late it is.
function Note({ d, delay }: { d: Departure, delay: number }) {
  if (d.Canceled) {
    return <span className="lirr-note canceled">Canceled</span>
  }
  if (d.Status) {
    const kind = d.Status === 'Held' ? 'late' : 'here'
    return <span className={`lirr-note ${kind}`}>{d.Status}</span>
  }
  if (delay >= LATE) {
    return <span className={`lirr-note ${delay >= VERY_LATE ? 'very-late' : 'late'}`}>{Math.round(delay / 60_000)} min late</span>
  }
  return <span />
}

// Row is a train: the minutes count down to its expected time, and the
// note at the end says if it is late, so that nothing before it moves.
function Row({ d, now }: { d: Departure, now: Date }) {
  const scheduled = new Date(d.Scheduled)
  const expected = d.Expected ? new Date(d.Expected) : null
  const delay = expected ? expected.getTime() - scheduled.getTime() : 0

  return (
    <div className={`lirr-row${d.Canceled ? ' canceled' : ''}`}>
      <span className="lirr-dot" style={{ background: d.Color }} />
      <span className="lirr-time">{format(scheduled, 'HH:mm')}</span>
      <span className="lirr-destination">{d.Destination}</span>
      {d.Canceled
        ? <><span /><span /></>
        : <>
            <Minutes minutes={minutesUntil(expected ?? scheduled, now)} />
            <span className="transit-unit">min</span>
          </>}
      <Note d={d} delay={delay} />
    </div>
  )
}

// Lirr is a timetable for one station: the next scheduled trains, with
// how late they are, cancellations, whether they are at the station, and
// any alerts. It shows whatever src (see lirrSign in the backend)
// returns, which lists trains until they leave.
function Lirr({ title, src, count, maxAlerts }: { title: string, src: string, count: number, maxAlerts: number }) {
  const board = usePoll<Board>(src, 10_000)
  const now = useTick(10_000)
  if (!board) {
    return null
  }

  // Show count trains that run, and any canceled ones among them.
  const rows: Departure[] = []
  let running = 0
  for (const d of board.Departures) {
    if (running === count) {
      break
    }
    rows.push(d)
    if (!d.Canceled) {
      running++
    }
  }

  return (
    <div className="transit-sign">
      <Heading title={title} />
      <div className="lirr-rows">
        {rows.map(d => <Row key={d.Scheduled + d.Train} d={d} now={now} />)}
      </div>
      <Alerts alerts={board.Alerts.slice(0, maxAlerts)} />
    </div>
  )
}

export default Lirr
