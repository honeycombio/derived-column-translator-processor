package derivedcolumnprocessor // import "github.com/honeycombio/derived-column-translator-processor/derivedcolumnprocessor"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"

	"github.com/honeycombio/derived-column-translator-processor/pkg/emit"
	"github.com/honeycombio/derived-column-translator-processor/pkg/honeycomb"
	"github.com/honeycombio/derived-column-translator-processor/pkg/translate"
)

type derivedColumnProcessor struct {
	cfg      *Config
	settings processor.Settings
	logger   *zap.Logger
	next     consumer.Traces

	client   *honeycomb.Client
	parser   ottl.Parser[*ottlspan.TransformContext]
	resolver translate.Resolver

	// seq holds the currently compiled statement sequence and is swapped
	// atomically by the refresh loop.
	seq atomic.Pointer[ottl.StatementSequence[*ottlspan.TransformContext]]

	// refreshMu serializes refresh and guards lastHash. lastHash is the hash of
	// the most recently compiled statement set; an unchanged hash skips recompile.
	refreshMu sync.Mutex
	lastHash  string

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newProcessor(set processor.Settings, cfg *Config, next consumer.Traces) *derivedColumnProcessor {
	return &derivedColumnProcessor{
		cfg:      cfg,
		settings: set,
		logger:   set.Logger,
		next:     next,
		resolver: translate.NewResolver(cfg.ColumnOverrides),
	}
}

func (p *derivedColumnProcessor) start(ctx context.Context, host component.Host) error {
	httpClient, err := p.cfg.ClientConfig.ToClient(ctx, host.GetExtensions(), p.settings.TelemetrySettings)
	if err != nil {
		return err
	}
	p.client = honeycomb.NewClient(
		string(p.cfg.APIKey),
		honeycomb.WithAPIURL(p.cfg.APIURL),
		honeycomb.WithHTTPClient(httpClient),
	)

	parser, err := ottlspan.NewParser(
		ottlfuncs.StandardFuncs[*ottlspan.TransformContext](),
		p.settings.TelemetrySettings,
	)
	if err != nil {
		return err
	}
	p.parser = parser

	// Initial load. Log and continue on failure; the refresh loop retries, so a
	// transient API error does not prevent the collector from starting.
	if err := p.refresh(ctx); err != nil {
		p.logger.Warn("initial derived column load failed; starting with no statements", zap.Error(err))
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	go p.refreshLoop(loopCtx)
	return nil
}

func (p *derivedColumnProcessor) refreshLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.refresh(ctx); err != nil {
				p.logger.Warn("derived column refresh failed; keeping previous statements", zap.Error(err))
			}
		}
	}
}

// refresh fetches the derived columns, translates them, and atomically swaps in
// the newly compiled statement sequence. If the generated statements are
// identical to the last successful refresh, it skips recompilation and the swap.
func (p *derivedColumnProcessor) refresh(ctx context.Context) error {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	cols, err := p.client.ListDerivedColumns(ctx, p.cfg.Dataset)
	if err != nil {
		return err
	}

	inputs := make([]emit.Input, len(cols))
	for i, c := range cols {
		inputs[i] = emit.Input{Alias: c.Alias, Expression: c.Expression}
	}
	out := emit.Generate(inputs, p.resolver)

	hash := hashStatements(out.Statements)
	if hash == p.lastHash && p.seq.Load() != nil {
		p.logger.Debug("derived columns unchanged; keeping compiled statements",
			zap.Int("derived_columns", len(cols)),
			zap.Int("statements", len(out.Statements)),
		)
		return nil
	}

	statements, err := p.parser.ParseStatements(out.Statements)
	if err != nil {
		return fmt.Errorf("parsing OTTL statements: %w", err)
	}
	seq := ottlspan.NewStatementSequence(
		statements,
		p.settings.TelemetrySettings,
		ottlspan.WithStatementSequenceErrorMode(p.cfg.ErrorMode),
	)
	p.seq.Store(&seq)
	p.lastHash = hash

	var skipped int
	for _, r := range out.Reports {
		if r.Skipped {
			skipped++
		}
	}
	p.logger.Info("applied derived columns",
		zap.Int("derived_columns", len(cols)),
		zap.Int("statements", len(out.Statements)),
		zap.Int("skipped", skipped),
	)
	return nil
}

// hashStatements returns a stable hash of the ordered statement list, used to
// detect whether the translated rules have changed since the last refresh.
func hashStatements(stmts []string) string {
	h := sha256.New()
	for _, s := range stmts {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (p *derivedColumnProcessor) processTraces(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	seq := p.seq.Load()
	if seq == nil {
		return td, nil // not yet loaded
	}

	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		sss := rs.ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			ss := sss.At(j)
			spans := ss.Spans()
			for k := 0; k < spans.Len(); k++ {
				tCtx := ottlspan.NewTransformContextPtr(rs, ss, spans.At(k))
				err := seq.Execute(ctx, tCtx)
				tCtx.Close()
				if err != nil {
					return td, err
				}
			}
		}
	}
	return td, nil
}

func (p *derivedColumnProcessor) shutdown(context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	return nil
}
