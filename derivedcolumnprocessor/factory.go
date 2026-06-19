package derivedcolumnprocessor // import "github.com/honeycombio/derived-column-translator-processor/derivedcolumnprocessor"

import (
	"context"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"

	"github.com/honeycombio/derived-column-translator-processor/pkg/honeycomb"
)

var componentType = component.MustNewType("derivedcolumn")

const defaultRefreshInterval = 5 * time.Minute

// NewFactory returns a factory for the derived column processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		componentType,
		createDefaultConfig,
		processor.WithTraces(createTraces, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		APIURL:          honeycomb.DefaultAPIURL,
		Dataset:         honeycomb.AllDatasets,
		RefreshInterval: defaultRefreshInterval,
		ErrorMode:       ottl.IgnoreError,
	}
}

func createTraces(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	next consumer.Traces,
) (processor.Traces, error) {
	p := newProcessor(set, cfg.(*Config), next)
	return processorhelper.NewTraces(
		ctx,
		set,
		cfg,
		next,
		p.processTraces,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
		processorhelper.WithStart(p.start),
		processorhelper.WithShutdown(p.shutdown),
	)
}
