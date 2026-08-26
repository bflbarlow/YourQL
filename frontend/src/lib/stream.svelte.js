// YourQL — shared streaming store for llm:stream events.
// One subscription + one state machine, consumed by both ConversationView
// (full mode) and MinimalistView. Adopts Minimalist's richer behavior:
//   - separate reasoning buffer from answer text
//   - gate: conversations with streaming_enabled off never show a bubble
import { EventsOn } from "../../wailsjs/runtime/runtime.js";

export function createStreamStore() {
  let active = $state(false);
  let text = $state("");
  let reasoningActive = $state(false);
  let reasoningText = $state("");
  let toolCards = $state([]); // [{name, args, status}]

  EventsOn("llm:stream", (data) => {
    const convId = getConversationId?.();
    if (!convId || data.conversation_id !== convId) return;
    const ev = data.event;
    switch (ev.type) {
      case "reasoning_start":
        active = true;
        reasoningActive = true;
        break;
      case "reasoning_end":
        reasoningActive = false;
        break;
      case "content_delta":
        active = true;
        if (reasoningActive) {
          reasoningText += ev.content;
        } else {
          text += ev.content;
        }
        break;
      case "tool_call_start":
        active = true;
        toolCards = [
          ...toolCards,
          { name: ev.tool_name || "tool", args: "", status: "forming" },
        ];
        break;
      case "tool_call_delta":
        toolCards = toolCards.map((c) =>
          c.name === ev.tool_name && ev.arguments
            ? { ...c, args: c.args + ev.arguments }
            : c,
        );
        break;
      case "tool_call_end":
        toolCards = toolCards.map((c) =>
          c.name === ev.tool_name ? { ...c, status: "executing" } : c,
        );
        break;
      case "done":
        reset();
        break;
    }
  });

  let getConversationId = null;

  function reset() {
    active = false;
    text = "";
    reasoningText = "";
    reasoningActive = false;
    toolCards = [];
  }

  return {
    get active() {
      return active;
    },
    get text() {
      return text;
    },
    get reasoningActive() {
      return reasoningActive;
    },
    get reasoningText() {
      return reasoningText;
    },
    get toolCards() {
      return toolCards;
    },
    bind(getter) {
      getConversationId = getter;
    },
    reset,
  };
}

// Auto-scroll an element to bottom when deps change or streaming deltas arrive.
export function attachAutoScroll(el, deps) {
  // Called from $effect in components: touch deps then scroll.
  if (el) {
    el.scrollTo({ top: el.scrollHeight, behavior: "instant" });
  }
}
