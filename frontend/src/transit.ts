import { useEffect, useState } from 'react'

// Shared types and hooks of the train signs.

export type Color = {
  Color: string
  TextColor: string
}

export type Alert = {
  Text: string // routes written as "[E]"
  Severe: boolean
}

// useTick returns the current time, updated every interval ms.
export function useTick(interval: number): Date {
  const [now, setNow] = useState(new Date())
  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), interval)
    return () => clearInterval(timer)
  }, [interval])
  return now
}

// usePoll fetches JSON from src now and every interval ms, keeping the last
// successful response.
export function usePoll<T>(src: string, interval: number): T | undefined {
  const [data, setData] = useState<T>()
  useEffect(() => {
    const fetchData = () => {
      fetch(src)
        .then(d => d.json())
        .then(d => setData(d))
        .catch(e => console.error(e))
    }
    fetchData()
    const timer = setInterval(fetchData, interval)
    return () => clearInterval(timer)
  }, [src, interval])
  return data
}

// minutesUntil is the whole number of minutes from now until t.
export function minutesUntil(t: Date, now: Date): number {
  return Math.floor((t.getTime() - now.getTime()) / 60_000)
}
