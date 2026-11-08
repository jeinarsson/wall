import { FC, useMemo, useState, useEffect, useCallback, CSSProperties } from 'react'
import { Calendar, dateFnsLocalizer, EventProps } from 'react-big-calendar'
import { format } from 'date-fns/format'
import { parse } from 'date-fns/parse'
import { startOfWeek } from 'date-fns/startOfWeek'
import { startOfDay } from 'date-fns/startOfDay'
import { getDay } from 'date-fns/getDay'
import { enUS } from 'date-fns/locale/en-US'

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

interface JsonEvent {
    Start: string;
    End: string;
    Summary: string;
    AllDay: boolean;
    Color: string;
}

const eventStart = (e: JsonEvent): Date => {
    if (e.AllDay) {
        return parse(e.Start, "yyyy-MM-dd", new Date());
    }
    return new Date(e.Start);
}

const eventEnd = (e: JsonEvent): Date => {
    if (e.AllDay) {
        const end = parse(e.End, "yyyy-MM-dd", new Date());
        return addDays(end, -1);  // RBC expects end-inclusive ranges, Google provides end-exclusive.
    }
    return new Date(e.End);
}

const EventContent: FC<EventProps<JsonEvent>> = ({ event, title, isAllDay, continuesPrior }) => {
    if (isAllDay || continuesPrior) {
        return <span className="event-title">{title}</span>;
    }
    return (
        <>
            <span className="event-dot"></span>
            <span className="event-time">{format(eventStart(event), 'HH:mm')}</span>
            <span className="event-title">{title}</span>
        </>
    )
}

const components = {
    event: EventContent,
}

const Month: FC = () => {
    const [now, setNow] = useState(new Date());
    const [eventData, setEventData] = useState<JsonEvent[]>([]);

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

    // Each event carries its calendar colour as a CSS variable; month.css decides how to use it.
    const today = startOfDay(now).getTime();
    const eventPropGetter = useCallback((event: JsonEvent) => ({
        className: eventEnd(event).getTime() < today ? 'event-past' : undefined,
        style: { '--event-color': event.Color } as CSSProperties,
    }), [today]);

    const dayPropGetter = useCallback((date: Date) => {
        const day = date.getDay();
        return { className: day === 0 || day === 6 ? 'rbc-weekend' : undefined };
    }, []);

    return (
        <Calendar
            view='month'
            onView={() => {}}
            date={now}
            onNavigate={() => {}}
            getNow={() => now}
            events={eventData}
            showAllEvents={true}
            startAccessor={eventStart}
            endAccessor={eventEnd}
            titleAccessor={(e: JsonEvent) => e.Summary}
            allDayAccessor={(e: JsonEvent) => e.AllDay}
            localizer={localizer}
            style={{ width: '100vw', height: '100vh' }}
            views={views}
            components={components}
            eventPropGetter={eventPropGetter}
            dayPropGetter={dayPropGetter}
            toolbar={false}
        />
    )
}

export default Month
