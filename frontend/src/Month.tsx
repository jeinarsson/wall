import { FC, useMemo, useState, useEffect } from 'react'
import { Calendar, dateFnsLocalizer } from 'react-big-calendar'
import { format } from 'date-fns/format'
import { parse } from 'date-fns/parse'
import { startOfWeek } from 'date-fns/startOfWeek'
import { getDay } from 'date-fns/getDay'
import { enUS } from 'date-fns/locale/en-US'

import './rbc-sass/styles.scss';

// @ts-ignore
import RollingMonthView from './CustomRbc/RollingMonthView' 
import { addDays } from 'date-fns'

const locales = {
    'en-US': enUS,
}
const startOfWeekMonday = (d: Date): Date => startOfWeek(d, { weekStartsOn: 1 })

// The types here are `object`. Strongly consider making them better as removing `locales` caused a fatal error
const localizer = dateFnsLocalizer({
    format,
    parse,
    startOfWeek: startOfWeekMonday,
    getDay,
    locales,
})
//@ts-ignore


interface JsonEvent {
    Start: string;
    End: string;
    Summary: string;
    AllDay: boolean;
}

const Month: FC = () => {
    const [now, setNow] = useState(new Date());
    const [eventData, setEventData] = useState([]);

    const fetchEvents = () => {
        fetch('/api/calendarEvents')
            .then(d => d.json())
            .then(d => setEventData(d))
            .catch(e => console.error(e));
    }
    useEffect(() => {
        const timer = setInterval(()=>setNow(new Date()), 60 * 1000);
        return () => {
            clearInterval(timer);
        };
    }, []);
    useEffect(()=>{
        fetchEvents();
    }, [now]);
    
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
            events={eventData}
            startAccessor={(e: JsonEvent) => new Date(e.Start)}
            endAccessor={(e: JsonEvent) => {
                let end = new Date(e.End);
                if (e.AllDay) {
                    end = addDays(end, -1);
                }
                return end;
            }
            }
            titleAccessor={(e: JsonEvent) => e.Summary}
            allDayAccessor={(e: JsonEvent) => e.AllDay}
            localizer={localizer}
            style={{ width: '100vw', height: '100vh' }}
            views={views}
            toolbar={false}
        />
    )
}

export default Month