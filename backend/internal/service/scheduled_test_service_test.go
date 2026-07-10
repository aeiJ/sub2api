package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestComputeScheduledTestNextRunIntervalJitterStaysWithinPlusMinusRange(t *testing.T) {
	from := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	plan := &ScheduledTestPlan{
		IntervalMinutes: 1,
		JitterSeconds:   10,
	}

	for i := 0; i < 100; i++ {
		next, err := computeScheduledTestNextRun(plan, from)
		require.NoError(t, err)
		require.False(t, next.Before(from.Add(50*time.Second)), "next_run_at should not be earlier than interval-jitter")
		require.False(t, next.After(from.Add(70*time.Second)), "next_run_at should not be later than interval+jitter")
	}
}

func TestScheduledTestRunnerInFlightGuardsPlanID(t *testing.T) {
	runner := NewScheduledTestRunnerService(nil, nil, nil, nil, nil)

	require.True(t, runner.tryAcquireInFlight(42))
	require.False(t, runner.tryAcquireInFlight(42))
	runner.releaseInFlight(42)
	require.True(t, runner.tryAcquireInFlight(42))
}
