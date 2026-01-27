# Bank Logos

## Setup

Add your bank logo SVG files to this directory with the following naming:
- `access-bank.svg`
- `gtbank.svg`
- `zenith.svg`
- etc.

## Usage in Router

```go
import "your-project/internal/middleware"

// In your main.go or router setup:
http.Handle("/static/bank-logos/", http.StripPrefix("/static/bank-logos/", 
    middleware.StaticFileServer("./static/bank-logos")))
```

## Fallback

If a logo file doesn't exist, a demo institution SVG is automatically returned.

## Cache Strategy

- Found logos: 30 days cache
- Demo fallback: 24 hours cache
