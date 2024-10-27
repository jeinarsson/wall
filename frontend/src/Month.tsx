import { FC, useMemo } from 'react'
import { Calendar, dateFnsLocalizer, Event } from 'react-big-calendar'
import {format} from 'date-fns/format'
import {parse} from 'date-fns/parse'
import {startOfWeek} from 'date-fns/startOfWeek'
import {getDay} from 'date-fns/getDay'
import {enUS} from 'date-fns/locale/en-US'
import {addHours} from 'date-fns/addHours'
import {startOfHour} from 'date-fns/startOfHour'

import './rbc-sass/styles.scss';
import RollingMonthView from './CustomRbc/RollingMonthView'
import { addDays } from 'date-fns'

const locales = {
  'en-US': enUS,
}
const endOfHour = (date: Date): Date => addHours(startOfHour(date), 1)
const startOfWeekMonday =  (d: Date): Date => startOfWeek(d, {weekStartsOn: 1})

// The types here are `object`. Strongly consider making them better as removing `locales` caused a fatal error
const localizer = dateFnsLocalizer({
  format,
  parse,
  startOfWeek: startOfWeekMonday,
  getDay,
  locales,
})
//@ts-ignore


const Month: FC = () => {
    const now = new Date()
    const start = endOfHour(now)
    const end = addHours(start, 2)
    const end2 = addDays(start, 2)
    const events = [
      {
        title: 'Learn cool stuff',
        start,
        end,
      },
      {
        title: 'All day',
        start: start,
        end: end2,
        allDay: true,
      },
    ]

    const { views } = useMemo(
        () => ({
          views: {
            month: RollingMonthView,
          },
        }),
        []
      )
  
    return (
      <Calendar
        defaultView='month'
        events={events}
        localizer={localizer}
        style={{ width: '100vw', height: '100vh' }}
        views={views}
        toolbar={false}
      />
    )
  }

export default Month