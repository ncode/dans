import { createRoot } from "react-dom/client";
import "@cloudscape-design/global-styles/index.css";
import { App } from "./App.tsx";
import "./style.css";
createRoot(document.getElementById("root")!).render(<App />);
