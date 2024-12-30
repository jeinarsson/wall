import { FC, useMemo, useState, useEffect, useCallback } from 'react'
import { Calendar, dateFnsLocalizer } from 'react-big-calendar'
import { format } from 'date-fns/format'
import { parse } from 'date-fns/parse'
import { startOfWeek } from 'date-fns/startOfWeek'
import { getDay } from 'date-fns/getDay'
import { enUS } from 'date-fns/locale/en-US'

import './rbc-sass/styles.scss';
import './month.css';

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

    const eventComponent = (props) => {
        if (props.isAllDay) {
            return props.title;
        }
        const start = new Date(props.event.Start);
        const startfmt = props.localizer.format(start, 'HH:mm');
        return (
        <div className="eventContainer">
            <div className="eventCalendarIndicatorContainer"><div className="eventCalendarIndicator" style={{borderColor: props.event.Color}}></div></div>
            <span className="eventTime">{startfmt}</span>
            <span className="eventTitle"> {props.title}</span>
        </div>
        )
    }

    const eventPropGetter = useCallback((event, start, end, isSelected) => {
    if (event.AllDay) {
        return {style: {backgroundColor: event.Color}}; 
    }
    return {};
    },[]);

    const components = useMemo(() => ({
        event: eventComponent, 
      }), [])    
    return (
        <Calendar
            defaultView='month'
            events={eventData}
            showAllEvents={true}
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
            components={components}
            eventPropGetter={eventPropGetter}
            toolbar={false}
        />
    )
}

export default Month