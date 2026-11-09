import { useEffect, useState } from 'react'
import { format } from 'date-fns/format'
import { getISOWeek } from 'date-fns/getISOWeek'

import Contours from './Contours.tsx'
import Lirr from './Lirr.tsx'
import Subway from './Subway.tsx'
import TrainMap, { View } from './TrainMap.tsx'

import './clock.css';

// Forest Hills on the left, Jamaica in the middle and the Atlantic Branch,
// where most trains come from, on the right.
const MAP_VIEW: View = { center: [-73.802, 40.7005], width: 11_500, rotate: 30 }

// useMinute returns the current time, updated exactly on each minute boundary.
function useMinute(): Date {
  const [now, setNow] = useState(new Date())
  useEffect(() => {
    const timer = setTimeout(() => setNow(new Date()), 60_000 - (Date.now() % 60_000) + 50)
    return () => clearTimeout(timer)
  }, [now])
  return now
}

function Top() {
  const now = useMinute()

  return (
    <div className="clock-screen">
      <Contours time={now} />
      <div className="clock">
        <div className="clock-time">
          <span className="clock-hours">{format(now, 'HH')}</span>
          <span className="clock-sep">:</span>
          <span className="clock-minutes">{format(now, 'mm')}</span>
        </div>
        <div className="clock-date">
          {format(now, 'EEEE d MMMM')}
          <span className="clock-week">week {getISOWeek(now)}</span>
        </div>
      </div>
      <div className="transit">
        <Subway title="Subway" src="/api/subway" count={3} maxAlerts={2} />
        <Lirr title="LIRR" src="/api/lirr" count={3} maxAlerts={2} />
      </div>
      <TrainMap src="/api/trainmap" basemap="/basemap.json" view={MAP_VIEW} />
    </div>
  )
}

export default Top
