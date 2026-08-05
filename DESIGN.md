---
name: ProjectBoard
description: A signal-desk interface for auditable human and Agent work.
colors:
  ink: "#171914"
  ink-soft: "#4f5349"
  canvas: "#e9ebe4"
  surface: "#f7f8f3"
  surface-raised: "#ffffff"
  line: "#cdd1c5"
  accent: "#b9f227"
  accent-ink: "#182000"
  danger: "#b93a2d"
  danger-ink: "#81231a"
  health: "#415d1a"
  placeholder: "#70766b"
  priority-high: "#8a6721"
  priority-medium: "#647866"
typography:
  display:
    fontFamily: "Bahnschrift SemiCondensed, Aptos Display, Segoe UI, sans-serif"
    fontSize: "clamp(1.75rem, 3vw, 3.25rem)"
    fontWeight: 800
    lineHeight: 0.98
    letterSpacing: "-0.035em"
  body:
    fontFamily: "Aptos, Segoe UI, PingFang SC, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 450
    lineHeight: 1.55
  body-small:
    fontFamily: "Aptos, Segoe UI, PingFang SC, sans-serif"
    fontSize: "0.82rem"
    fontWeight: 500
    lineHeight: 1.5
  title:
    fontFamily: "Bahnschrift SemiCondensed, Aptos Display, Segoe UI, sans-serif"
    fontSize: "clamp(1.65rem, 2.8vw, 3.35rem)"
    fontWeight: 800
    lineHeight: 0.98
    letterSpacing: "-0.035em"
  label:
    fontFamily: "Aptos Mono, Cascadia Code, monospace"
    fontSize: "0.6875rem"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "0.06em"
  label-small:
    fontFamily: "Aptos Mono, Cascadia Code, monospace"
    fontSize: "0.62rem"
    fontWeight: 700
    lineHeight: 1.2
rounded:
  micro: "2px"
  control: "6px"
  control-large: "8px"
  surface: "12px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "40px"
components:
  button-primary:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.surface}"
    rounded: "{rounded.control}"
    padding: "10px 14px"
  button-primary-hover:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-ink}"
    rounded: "{rounded.control}"
  input:
    backgroundColor: "{colors.surface-raised}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "11px 12px"
  badge:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-soft}"
    rounded: "{rounded.pill}"
    padding: "4px 8px"
---

# Design System: ProjectBoard

## Overview

**Creative North Star: "The Dispatch Signal Desk"**

ProjectBoard behaves like a physical operations desk for software work: one continuous surface, clearly labeled controls, visible routes, and unmistakable signal changes. The interface is dense enough for daily operation but keeps each screen centered on one decision. It rejects both generic card dashboards and theatrical neon hacker styling.

**Key Characteristics:**

- Continuous work surfaces separated by alignment and hairlines, not card mosaics.
- A cold daylight palette with one rare signal color.
- Condensed, forceful headings paired with calm workhorse body copy.
- Stage, branch, actor, and evidence are treated as operational notation.

## Colors

The palette resembles powder-coated equipment under neutral office light.

### Primary

- **Signal Lime:** Reserved for current routes, selected navigation, and live progress. It is never decorative.

### Neutral

- **Graphite:** Primary text and decisive controls.
- **Workbench:** App shell and inactive navigation field.
- **Instrument White:** Main working surface and raised drawers.
- **Calibration Line:** Dividers, tracks, and input boundaries.

### Named Rules

**The Signal Rarity Rule.** Lime means selected, current, or ready for action. If an element carries no state, it does not receive the accent.

## Typography

**Display Font:** Bahnschrift SemiCondensed or Aptos Display with system fallbacks  
**Body Font:** Aptos or Segoe UI with PingFang SC fallback  
**Label/Mono Font:** Aptos Mono or Cascadia Code

**Character:** Headings are compressed like equipment labels so dense information can retain scale. Body text stays conventional and readable. Monospace is limited to IDs, branches, versions, code, and measurements.

### Named Rules

**The Notation Rule.** Monospace communicates machine-addressable information, never technical atmosphere.

## Layout

Desktop uses a narrow global mast, a 248px navigation rail, and one uninterrupted working surface. Page titles sit in an asymmetric control header with actions aligned to the operational edge. Queue rows use a strict metadata, title, route, and assignee grid. At 840px the rail becomes a horizontal control strip; below 600px actions wrap, tables become single-column, and the five-stage route remains horizontally scrollable.

## Elevation & Depth

The system is flat by default. Tonal surfaces and one-pixel calibration lines define hierarchy. Only drawers and authentication panels lift above the workbench with a soft, offset graphite-tinted shadow.

**The Flat Instrument Rule.** Resting content has no shadow. Elevation indicates temporary focus or protected interaction.

## Shapes

Controls use compact 6px corners. Structural surfaces use 12px corners only when they detach from the main plane. Status badges are pills because they are small categorical tokens, not containers.

## Components

### Buttons

Primary buttons are graphite with light text and switch to signal lime on hover. Secondary controls are transparent or white with a calibration-line border. Active feedback translates down by one pixel; keyboard focus uses a visible two-ring signal outline.

### Inputs / Fields

Fields are instrument white with 6px corners and explicit labels above. Focus changes the border to graphite and adds a restrained outer ring. Error fields use the danger family and include recovery text.

### Navigation

Every route uses a Lucide icon and label. The active route receives a full lime field and graphite text; hover uses instrument white. Global and project navigation remain spatially distinct.

### Work Queue Row

Rows are cardless and separated by one bottom hairline. Priority is expressed by a short calibrated bar plus text metadata. Hover shifts the row toward instrument white and reveals its trailing affordance.

### Stage Route

The five-stage component is the signature pattern: a continuous track with completed, current, and upcoming nodes differentiated by shape, fill, icon, and text. Blocked is an attached warning on the current node, never a replacement stage.

## Do's and Don'ts

### Do:

- **Do** use one strong alignment grid per screen.
- **Do** keep operational state visible without opening a drawer.
- **Do** reserve lime for semantic signal and primary focus.
- **Do** preserve full Chinese and English labels at narrow widths.

### Don't:

- **Don't** build dashboard mosaics from equal cards.
- **Don't** use gradients, glass, neon glow, or decorative status dots.
- **Don't** use icons without labels for primary navigation.
- **Don't** collapse blocked, abandoned, or assignee state into color alone.
