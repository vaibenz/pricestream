// Package source produces quotes from market data feeds.
package source

import (
	"context"

	"github.com/vaibenz/pricestream/internal/quote"
)

// Source streams quotes into out until ctx is cancelled. Implementations
// handle their own reconnects and only return on cancellation or a
// configuration error they cannot recover from.
type Source interface {
	Run(ctx context.Context, out chan<- quote.Quote) error
}
