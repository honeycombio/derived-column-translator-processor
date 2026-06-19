package derivedcolumnprocessor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor/processortest"
)

// newTraces builds a single span carrying a duration_ms attribute.
func newTraces(durationMs int64) ptrace.Traces {
	td := ptrace.NewTraces()
	span := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test-span")
	span.Attributes().PutInt("duration_ms", durationMs)
	return td
}

func firstSpan(td ptrace.Traces) ptrace.Span {
	return td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
}

func TestProcessorTranslatesAndHotSwaps(t *testing.T) {
	var body atomic.Value
	body.Store(`[{"id":"1","alias":"is_slow","expression":"$duration_ms > 1000"}]`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer srv.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.APIKey = "test-key"
	cfg.APIURL = srv.URL
	cfg.RefreshInterval = time.Hour // we drive refresh manually

	p := newProcessor(processortest.NewNopSettings(componentType), cfg, consumertest.NewNop())
	ctx := context.Background()
	require.NoError(t, p.start(ctx, componenttest.NewNopHost()))
	defer func() { require.NoError(t, p.shutdown(ctx)) }()

	// Initial load: is_slow is true for a slow span.
	out, err := p.processTraces(ctx, newTraces(2000))
	require.NoError(t, err)
	v, ok := firstSpan(out).Attributes().Get("is_slow")
	require.True(t, ok, "is_slow should be set")
	assert.True(t, v.Bool())

	// A fast span gets is_slow=false.
	out, err = p.processTraces(ctx, newTraces(10))
	require.NoError(t, err)
	v, ok = firstSpan(out).Attributes().Get("is_slow")
	require.True(t, ok)
	assert.False(t, v.Bool())

	// Hot-swap: replace the derived column set and refresh.
	body.Store(`[{"id":"2","alias":"is_fast","expression":"$duration_ms < 100"}]`)
	require.NoError(t, p.refresh(ctx))

	out, err = p.processTraces(ctx, newTraces(50))
	require.NoError(t, err)
	span := firstSpan(out)
	v, ok = span.Attributes().Get("is_fast")
	require.True(t, ok, "is_fast should be set after hot-swap")
	assert.True(t, v.Bool())
	_, ok = span.Attributes().Get("is_slow")
	assert.False(t, ok, "is_slow should no longer be applied after hot-swap")
}

func TestRefreshSkipsRecompileWhenUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"1","alias":"is_slow","expression":"$duration_ms > 1000"}]`))
	}))
	defer srv.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.APIKey = "test-key"
	cfg.APIURL = srv.URL
	cfg.RefreshInterval = time.Hour

	p := newProcessor(processortest.NewNopSettings(componentType), cfg, consumertest.NewNop())
	ctx := context.Background()
	require.NoError(t, p.start(ctx, componenttest.NewNopHost()))
	defer func() { require.NoError(t, p.shutdown(ctx)) }()

	first := p.seq.Load()
	require.NotNil(t, first)

	// Same derived columns on the next refresh: the compiled sequence must not
	// be swapped (identical pointer).
	require.NoError(t, p.refresh(ctx))
	assert.Same(t, first, p.seq.Load(), "unchanged rules should not recompile")
}

func TestConfigValidate(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	require.Error(t, cfg.Validate(), "missing api_key should fail")

	cfg.APIKey = "k"
	require.NoError(t, cfg.Validate())

	cfg.RefreshInterval = 0
	require.Error(t, cfg.Validate(), "zero refresh_interval should fail")
}

func TestFactoryCreatesProcessor(t *testing.T) {
	f := NewFactory()
	cfg := f.CreateDefaultConfig().(*Config)
	cfg.APIKey = "k"
	_, err := f.CreateTraces(context.Background(), processortest.NewNopSettings(componentType), cfg, consumertest.NewNop())
	require.NoError(t, err)
}
