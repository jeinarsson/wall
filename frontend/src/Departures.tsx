import { useEffect, useState } from 'react'
import { format } from 'date-fns/format'

import './departures.css';

type Departure = {
  Route: string // empty for LIRR branches
  RouteName: string
  Color: string
  TextColor: string
  Destination: string
  Time: string // expected
  Scheduled: string
  Delay: number // seconds
}

type Board = {
  Name: string
  Count: number
  ShowSchedule: boolean
  ShowDestination: boolean
  Departures: Departure[]
}

// Delays from which the expected time is shown, and turns red.
const LATE = 60
const VERY_LATE = 5 * 60

// useTick returns the current time, updated every interval ms.
function useTick(interval: number): Date {
  const [now, setNow] = useState(new Date())
  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), interval)
    return () => clearInterval(timer)
  }, [interval])
  return now
}

function Row({ d, board, now }: { d: Departure, board: Board, now: Date }) {
  const time = new Date(d.Time)
  const minutes = Math.floor((time.getTime() - now.getTime()) / 60_000)
  const lateness = d.Delay >= VERY_LATE ? 'very-late' : d.Delay >= LATE ? 'late' : ''

  return (
    <div className="departure">
      {d.Route
        ? <span className="departure-bullet" style={{ background: d.Color, color: d.TextColor }}>{d.Route}</span>
        : <span className="departure-bullet"><span className="departure-dot" style={{ background: d.Color }} /></span>}
      {board.ShowSchedule && (
        <span className="departure-time">
          {format(new Date(d.Scheduled), 'HH:mm')}
          {lateness && <span className={`departure-expected ${lateness}`}>{format(time, 'HH:mm')}</span>}
        </span>
      )}
      {board.ShowDestination && (
        <span className={`departure-destination${board.ShowSchedule ? '' : ' wide'}`}>{d.Destination}</span>
      )}
      <span className="departure-minutes">
        {minutes < 1 ? 'now' : <>{minutes}<span className="departure-unit">min</span></>}
      </span>
    </div>
  )
}

function Departures() {
  const [boards, setBoards] = useState<Board[]>([])
  const now = useTick(10_000)

  useEffect(() => {
    const fetchDepartures = () => {
      fetch('/api/departures')
        .then(d => d.json())
        .then(d => setBoards(d))
        .catch(e => console.error(e))
    }
    fetchDepartures()
    const timer = setInterval(fetchDepartures, 30_000)
    return () => clearInterval(timer)
  }, [])

  return (
    <div className="departures">
      {boards.map(board => {
        // The backend only refreshes every minute; drop trains that have
        // left since then.
        const upcoming = board.Departures
          .filter(d => new Date(d.Time) > now)
          .slice(0, board.Count)
        if (upcoming.length === 0) {
          return null
        }
        return (
          <div className="departure-board" key={board.Name}>
            {upcoming.map(d => <Row key={d.Time + d.Route + d.Destination} d={d} board={board} now={now} />)}
          </div>
        )
      })}
    </div>
  )
}

export default Departures
