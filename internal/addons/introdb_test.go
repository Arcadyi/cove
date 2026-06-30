package addons

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ms(v int64) *int64 { return &v }

func TestMergeTimestamps_BaseWins(t *testing.T) {
	base := &TimestampData{
		Intro: []TimestampSegment{{StartMs: ms(1000), EndMs: ms(90000)}},
	}
	fill := &TimestampData{
		Intro: []TimestampSegment{{StartMs: ms(2000), EndMs: ms(80000)}},
		Recap: []TimestampSegment{{StartMs: ms(0), EndMs: ms(30000)}},
	}
	out := mergeTimestamps(base, fill)
	require.NotNil(t, out)
	// base intro is preserved
	assert.Equal(t, base.Intro, out.Intro)
	// fill recap is adopted (base had none)
	assert.Equal(t, fill.Recap, out.Recap)
}

func TestMergeTimestamps_FillAdopted(t *testing.T) {
	base := &TimestampData{}
	fill := &TimestampData{
		Intro:   []TimestampSegment{{StartMs: ms(5000), EndMs: ms(95000)}},
		Credits: []TimestampSegment{{StartMs: ms(3300000), EndMs: ms(3400000)}},
		Preview: []TimestampSegment{{StartMs: ms(3400000), EndMs: ms(3500000)}},
	}
	out := mergeTimestamps(base, fill)
	assert.Equal(t, fill.Intro, out.Intro)
	assert.Equal(t, fill.Credits, out.Credits)
	assert.Equal(t, fill.Preview, out.Preview)
}

func TestMergeTimestamps_AllFieldsCovered(t *testing.T) {
	base := &TimestampData{
		Intro:   []TimestampSegment{{StartMs: ms(1000), EndMs: ms(2000)}},
		Recap:   []TimestampSegment{{StartMs: ms(0), EndMs: ms(500)}},
		Credits: []TimestampSegment{{StartMs: ms(5000), EndMs: ms(6000)}},
		Preview: []TimestampSegment{{StartMs: ms(6000), EndMs: ms(7000)}},
	}
	fill := &TimestampData{
		Intro:   []TimestampSegment{{StartMs: ms(9999), EndMs: ms(99999)}},
		Recap:   []TimestampSegment{{StartMs: ms(9999), EndMs: ms(99999)}},
		Credits: []TimestampSegment{{StartMs: ms(9999), EndMs: ms(99999)}},
		Preview: []TimestampSegment{{StartMs: ms(9999), EndMs: ms(99999)}},
	}
	out := mergeTimestamps(base, fill)
	// base has everything; fill should be entirely ignored
	assert.Equal(t, base.Intro, out.Intro)
	assert.Equal(t, base.Recap, out.Recap)
	assert.Equal(t, base.Credits, out.Credits)
	assert.Equal(t, base.Preview, out.Preview)
}
