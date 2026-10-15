# Accessibility Statement

FuzeCLI aims to make local-first AI-assisted development usable by as many developers as possible, including people who use assistive technology or alternative input methods.

## Commitment

Accessibility is an ongoing engineering responsibility. We aim to:

- Keep primary workflows navigable by keyboard, with visible focus indicators.
- Use clear labels, headings, status messages, and actionable error descriptions.
- Maintain readable contrast and avoid relying on color alone to communicate state.
- Support responsive layouts and browser zoom without hiding essential actions.
- Prefer semantic HTML and accessible names for controls in the Studio.
- Make CLI output understandable without depending on color, animation, or timing.

These are goals, not a claim that every part of FuzeCLI currently conforms to WCAG 2.2 AA. The project has not yet completed a comprehensive independent accessibility audit, and individual screens or terminal workflows may have barriers.

## Supported environments

FuzeCLI is primarily developed and tested for Windows. Its embedded Studio runs in a modern browser, and the repository CI validates the embedded JavaScript syntax. CI is not currently a substitute for manual keyboard, screen-reader, contrast, or browser-zoom testing across platforms.

The command-line interface may be used with terminal accessibility features provided by the user's operating system and terminal emulator. Experience can vary by terminal, screen reader, and color configuration.

## Known limitations and verification

- Full keyboard-only navigation and screen-reader behavior across all Studio screens have not been independently verified.
- Color contrast, reduced-motion preferences, zoom/reflow, and accessible announcements for streaming or background status updates need systematic testing.
- The terminal's interactive behavior depends partly on the terminal emulator and platform.
- Accessibility of provider-generated content and third-party tools is outside FuzeCLI's direct control.

We welcome focused reports that identify the affected screen or command, the steps to reproduce the barrier, the expected behavior, and the actual behavior. Please avoid including API keys, private source code, or other sensitive data in reports.

## Reporting an accessibility barrier

Open a GitHub issue at https://github.com/Enoch7768/fuzecli/issues and include the label `accessibility` if available. If you cannot use the issue tracker because of the barrier itself, contact the maintainers through the repository's available contact channels.

When practical, include your operating system, browser or terminal, assistive technology and version, keyboard steps, and a screenshot or short recording with sensitive information removed.

## Improvement process

Accessibility reports will be reviewed alongside other bugs and prioritized by impact on completing essential tasks. Contributions that improve semantics, keyboard support, contrast, error clarity, or assistive-technology feedback are welcome. Please see [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a change.
