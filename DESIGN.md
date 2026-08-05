---
name: ProjectBoard
description: A calm, precise operations workspace for auditable human and Agent work.
colors:
  ink: "#17191f"
  ink-soft: "#606571"
  canvas: "#f4f5f7"
  surface: "#ffffff"
  surface-subtle: "#f8f9fb"
  line: "#dfe2e8"
  line-strong: "#c9cdd6"
  accent: "#536fd7"
  accent-soft: "#e9edfb"
  accent-ink: "#304cae"
  danger: "#b84646"
  health: "#2f8065"
typography:
  display:
    fontFamily: "Segoe UI, Microsoft YaHei UI, PingFang SC, sans-serif"
    fontSize: "clamp(2rem, 3vw, 3rem)"
    fontWeight: 760
    lineHeight: 1.05
    letterSpacing: "-0.03em"
  body:
    fontFamily: "Segoe UI, Microsoft YaHei UI, PingFang SC, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 450
    lineHeight: 1.6
  label:
    fontFamily: "Segoe UI, Microsoft YaHei UI, PingFang SC, sans-serif"
    fontSize: "0.6875rem"
    fontWeight: 650
    lineHeight: 1.3
rounded:
  control: "6px"
  surface: "10px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "40px"
  page: "56px"
---

# Design System: ProjectBoard

## Creative North Star: The Quiet Control Room

ProjectBoard is an engineering operations surface used for long sessions under ordinary office light. It should feel calm, factual, and immediately scannable: a fixed white navigation rail beside a cool-gray workspace, with one restrained blue signal for selection and action. Product truth comes from state, responsibility, evidence, and history—not decoration.

## Visual Language

- Use uninterrupted work surfaces, generous whitespace, and hairline dividers instead of card mosaics.
- Use near-black headings with conventional UI typography; dense metadata may use the mono stack only when it is genuinely machine-addressable.
- Reserve blue for current navigation, focus, primary actions, and live route state. It is never decorative.
- Use green for healthy/complete status and red only for danger or blocking conditions.
- Resting content is flat. Only drawers and protected transient layers receive elevation.

## Layout

Desktop uses a 228px full-height white navigation rail and a cool-gray workspace. Primary content has 56px page gutters and a readable maximum line length. Page headings are part of the content flow and use a bottom divider. Lists are full-width, cardless rows with a clear trailing affordance. At 840px the rail becomes a compact horizontal navigation strip; below 620px list metadata collapses without hiding the primary identity or state.

## Components

### Navigation

Section labels are quiet and sentence-case. The active route uses a pale blue field, blue icon/text, and a one-pixel blue edge. Hover uses a subtle cool-gray fill. Lucide icons accompany all routes.

### Buttons and Fields

Primary buttons use the accent blue and white text. Secondary buttons are white with a neutral border. Inputs are white, 44px high, and gain a blue border plus a restrained focus ring. Disabled state remains legible and visibly inactive.

### Lists and Drawers

Rows are divided by hairlines and shift slightly on hover to reveal clickability. A right-side drawer is the consistent inspector/settings surface for management entities. It preserves the list context, groups identity, access, and security by dividers, and keeps dangerous actions visually separate.

### Task Route

The five-stage route remains ProjectBoard’s signature control. Completed stages use ink, the current stage uses blue with a soft outer field, and future stages remain neutral. Blocked status is an attached warning and never replaces the main stage.

## Motion

Motion is limited to context changes: the drawer enters with a quick exponential ease-out; list arrows shift on hover; buttons and navigation use short color transitions. `prefers-reduced-motion` removes transform-based movement.

## Accessibility

Keyboard focus is always visible. Status never relies on color alone. Text and controls meet WCAG AA contrast. Drawers retain dialog semantics and close through their labeled control, backdrop, or Escape. Responsive layouts preserve full labels and reachable actions.
