# Case Studio

A small, fully offline phone-case designer: paint on the back of a phone (brush, airbrush, eraser, fill, line, rectangle, ellipse, picker), pick colors from a palette, and export the design as a JPEG.

The whole app is one file (`index.html`). `main.go` embeds it in a single binary that serves it on `127.0.0.1` and opens your browser, so it needs no install and no internet.

## Run

Download the binary for your system from the Releases page and run it, or open `index.html` directly in a browser.

## Build

```sh
go build -trimpath -ldflags "-s -w" -o case-studio .
```

Options: `-port 8080`, `-no-browser`, `-keep-alive`.

Shortcuts: B brush, A airbrush, E eraser, G fill, L line, R rectangle, O ellipse, I picker, `[` `]` size, Ctrl+Z / Ctrl+Y undo/redo, Ctrl+S export.
