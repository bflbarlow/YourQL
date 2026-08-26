# F-15 · Refresh outdated model references in README

**Severity:** Low · **Effort:** Trivial · **Risk categories:** user trust, appeal (secondary goals)

## Problem Statement

`README.md` provider table reads:

> | **Anthropic** | Claude 3 Opus, Claude 3 Sonnet, Claude 3 Haiku |
> | **OpenAI** | GPT-4, GPT-4 Turbo, GPT-3.5 Turbo, and compatible APIs |

Model naming moves faster than READMEs; by the time a reader evaluates the
product these lists read as stale, and staleness in documentation subtly
undermines the product's core pitch — *accuracy and currency*. The technical
reality is better than the copy: the custom-endpoint path accepts any
OpenAI-compatible model, and Anthropic/OpenAI HTTP integration passes
whatever model string the user configures.

## Solution Design

Replace enumerated model lists with capability statements that age well:

> | **Anthropic** | Any Claude model via the Anthropic Messages API |
> | **OpenAI** | Any GPT-model family via the Chat Completions API (OpenRouter, Azure OpenAI, etc.) |
> | **Ollama** | Local, self-hosted models via Ollama |
> | **Custom Endpoint** | Any OpenAI-compatible HTTP API — LM Studio, llama.cpp server, vLLM, cloud-hosted models |

Optionally add a maintenance rule to prevent recurrence:

- In `documentation/AGENT_READ_FIRST.md` quick-reference or the README top
  comment: *"README avoids enumerating specific model versions; describe
  families/APIs instead."*
- Since the frontend likely has provider-type dropdown help text too, sweep
  once: `grep -rn "Claude 3\|GPT-4\|GPT-3.5" frontend/src README.md` and
  soften hits the same way (keep concrete examples where they aid setup UX,
  e.g., placeholder text in the model field — placeholders are expected to
  be examples and age fine; prose claims are what rot).

## Implementation Plan

1. Edit README table per above.
2. Sweep grep; adjust prose claims; leave input placeholders/examples alone.
3. No release needed; bundle with the next housekeeping PR (F-20).

## Risk Assessment

None — copy-only change. Charter §4.3 case 3. Improves appeal/trust (#4)
with zero risk to #1–#3.
