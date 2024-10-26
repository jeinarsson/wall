import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import {
  createBrowserRouter,
  RouterProvider,
} from "react-router-dom";

import './normalize.css';

import Bottom from './Bottom.tsx'
import Top from './Top.tsx'

const router = createBrowserRouter([
  {
    path: "/",
    element: <h1>/</h1>,
  },
  {
    path: "/top",
    element: <Top/>,
  },
  {
    path: "/bottom",
    element: <Bottom/>,
  },
]);

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
