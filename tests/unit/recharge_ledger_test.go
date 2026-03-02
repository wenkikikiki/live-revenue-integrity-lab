package unit

import (
	"testing"

	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
)

func TestRechargeLedgerEntriesBalanced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		coins uint32
	}{
		{coins: 1},
		{coins: 100},
		{coins: 100000},
	}

	for _, tc := range tests {
		entries := service.BuildRechargeEntries(99, tc.coins)
		if sum := service.SumLedgerEntries(entries); sum != 0 {
			t.Fatalf("ledger sum must be zero, got %d for coins=%d", sum, tc.coins)
		}
	}
}
