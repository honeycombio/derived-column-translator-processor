package derivedcolumnprocessor // import "github.com/honeycombio/derived-column-translator-processor/derivedcolumnprocessor"

import (
	"errors"
	"fmt"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configopaque"
)

// Config configures the derived column processor.
type Config struct {
	// APIKey is the Honeycomb Configuration Key used to read derived columns. It
	// must have the "Manage Queries and Columns" permission. This is a v1
	// Configuration Key (sent in the X-Honeycomb-Team header), not an ingest key
	// and not a v2 Management Key: derived columns are only exposed on the v1
	// Configuration API.
	APIKey configopaque.String `mapstructure:"api_key"`

	// APIURL is the Honeycomb API base URL (defaults to the US instance).
	APIURL string `mapstructure:"api_url"`

	// Dataset is the dataset slug to read derived columns from, or "__all__"
	// for environment-wide derived columns.
	Dataset string `mapstructure:"dataset"`

	// RefreshInterval is how often to re-fetch derived columns and recompile.
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`

	// ErrorMode controls how OTTL statement execution errors are handled
	// (ignore, silent, or propagate).
	ErrorMode ottl.ErrorMode `mapstructure:"error_mode"`

	// ColumnOverrides maps a Honeycomb column name to an explicit OTTL path,
	// e.g. {"service.name": "resource.attributes[\"service.name\"]"}.
	ColumnOverrides map[string]string `mapstructure:"column_overrides"`

	confighttp.ClientConfig `mapstructure:",squash"`
}

var _ component.Config = (*Config)(nil)

// Validate checks the configuration.
func (cfg *Config) Validate() error {
	if cfg.APIKey == "" {
		return errors.New("api_key is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh_interval must be positive")
	}
	switch cfg.ErrorMode {
	case ottl.IgnoreError, ottl.PropagateError, ottl.SilentError:
	default:
		return fmt.Errorf("invalid error_mode %q", cfg.ErrorMode)
	}
	return nil
}
