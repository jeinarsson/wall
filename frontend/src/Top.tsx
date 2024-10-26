import { useState } from 'react'

function Top() {
  const [count, setCount] = useState(0)

  return (
    <>
      <h1>Top screen</h1>
      <div className="card">
        <button onClick={() => setCount((count) => count + 1)}>
          count is {count}
        </button>
      </div>
    </>
  )
}

export default Top
