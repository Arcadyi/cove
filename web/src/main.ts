import { mount } from "svelte";

import "./assets/app.css";

import App from "./renderer/src/App.svelte";

const app = mount(App, {
  target: document.getElementById("app")!,
});

export default app;
