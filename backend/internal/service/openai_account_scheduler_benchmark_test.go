package service

import (
	"testing"
)

func buildOpenAISchedulerBenchmarkCandidates(size int) []openAIAccountCandidateScore {
	if size <= 0 {
		return nil
	}
	candidates := make([]openAIAccountCandidateScore, 0, size)
	for i := 0; i < size; i++ {
		accountID := int64(10_000 + i)
		candidates = append(candidates, openAIAccountCandidateScore{
			account: &Account{
				ID:       accountID,
				Priority: i % 7,
			},
			loadInfo: &AccountLoadInfo{
				AccountID:    accountID,
				LoadRate:     (i * 17) % 100,
				WaitingCount: (i * 11) % 13,
			},
			score:     float64((i*29)%1000) / 100,
			errorRate: float64((i * 5) % 100 / 100),
			ttft:      float64(30 + (i*3)%500),
			hasTTFT:   i%3 != 0,
		})
	}
	return candidates
}

func BenchmarkOpenAIDedicatedSelectionOrder(b *testing.B) {
	cases := []struct {
		name string
		size int
	}{
		{name: "n_16", size: 16},
		{name: "n_64", size: 64},
		{name: "n_256", size: 256},
	}

	for _, tc := range cases {
		candidates := buildOpenAISchedulerBenchmarkCandidates(tc.size)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := sortOpenAIDedicatedSelectionOrder(candidates)
				if len(result) == 0 {
					b.Fatal("unexpected empty result")
				}
			}
		})
	}
}
