import { mount } from "svelte";
import App from "./App.svelte";
import { GetGeneralSettings } from "../wailsjs/go/main/App.js";
import { initFromStorage } from "./lib/theme.js";
import { initHtmlBridge } from "./lib/htmlBridge.js";
import "./variables.css";
import "./minimalist.css";

// Theme / accent / scale — owned by lib/theme.js.
initFromStorage(GetGeneralSettings);

// Single bridge for backend-rendered assistant HTML interactions
// (copy SQL, sort tables, SQL section toggles). Replaces the old
// window.* duplicates in main.js + ConversationView and the
// body-wide MutationObserver.
initHtmlBridge();

mount(App, {
  target: document.getElementById("app"),
});
