import { useState } from 'react'

function Bottom() {
  const [count, setCount] = useState(0)

  return (
    <>
      <h1>Bottom screen</h1>
      <div className="card">
        <button onClick={() => setCount((count) => count + 1)}>
          count is {count}
        </button>
      </div>
    </>
  )
}

export default Bottom
