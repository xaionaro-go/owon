package owonusb

import (
	"fmt"
	"strings"
	"time"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
	"github.com/xaionaro-go/owon/pkg/owonsession"
)

const (
	// DefaultVendorID is the observed OWON HDS2202S USB vendor identifier.
	//
	// Example: Config with zero VendorID selects 0x5345.
	DefaultVendorID owonmodel.VendorID = 0x5345
	// DefaultProductID is the observed OWON HDS2202S USB product identifier.
	//
	// Example: Config with zero ProductID selects 0x1234.
	DefaultProductID owonmodel.ProductID = 0x1234
	// DefaultModel is the SCPI model required for this single-device service.
	//
	// Example: an omitted ExpectedModel still validates HDS2202S after reconnect.
	DefaultModel = "HDS2202S"
	// defaultInEndpoint is the bulk IN endpoint number observed on the instrument.
	//
	// Example: address 0x81 corresponds to endpoint number one.
	defaultInEndpoint = 1
	// defaultOutEndpoint is the bulk OUT endpoint number observed on the instrument.
	//
	// Example: address 0x01 corresponds to endpoint number one.
	defaultOutEndpoint = 1
	// maximumUSBIdentifier is the largest value representable in a USB descriptor ID.
	//
	// Example: 0x10000 is rejected before narrowing to gousb.ID.
	maximumUSBIdentifier = 0xffff
)

// Config selects one instrument and bounds its response allocation and device operations.
// OperationTimeout zero selects owonsession.DefaultDeviceOperationTimeout; native cancellation
// remains cooperative and never relinquishes resources before completion.
//
// Example: empty Serial selects the sole VID/PID match; multiple matches require an explicit Serial.
type Config struct {
	VendorID             owonmodel.VendorID
	ProductID            owonmodel.ProductID
	Serial               owonmodel.SerialNumber
	ExpectedModel        string
	MaximumResponseBytes uint32
	OperationTimeout     time.Duration
}

// withDefaults fills stable descriptor and allocation defaults.
//
// Example: an omitted VID/PID becomes 5345:1234.
func (config Config) withDefaults() Config {
	config.Serial = owonmodel.SerialNumber(strings.TrimSpace(string(config.Serial)))
	config.ExpectedModel = strings.TrimSpace(config.ExpectedModel)
	if config.VendorID == 0 {
		config.VendorID = DefaultVendorID
	}
	if config.ProductID == 0 {
		config.ProductID = DefaultProductID
	}
	if config.MaximumResponseBytes == 0 {
		config.MaximumResponseBytes = owonprotocol.DefaultMaximumResponseBytes
	}
	if strings.TrimSpace(config.ExpectedModel) == "" {
		config.ExpectedModel = DefaultModel
	}

	return config
}

// validateUSBConfig rejects values that cannot be represented by USB descriptors.
//
// Example: a vendor identifier above 65535 fails before conversion to gousb.ID.
func validateUSBConfig(config Config) error {
	if config.VendorID > maximumUSBIdentifier {
		return &ErrInvalidConfig{Reason: fmt.Sprintf("USB vendor ID %#x exceeds 16 bits", config.VendorID)}
	}
	if config.ProductID > maximumUSBIdentifier {
		return &ErrInvalidConfig{Reason: fmt.Sprintf("USB product ID %#x exceeds 16 bits", config.ProductID)}
	}
	if strings.TrimSpace(config.ExpectedModel) == "" {
		return &ErrInvalidConfig{Reason: "expected SCPI model is required"}
	}
	if _, err := owonprotocol.NormalizeResponseLimit(config.MaximumResponseBytes); err != nil {
		return fmt.Errorf("validate USB maximum response: %w", err)
	}
	if _, err := owonsession.NormalizeOperationTimeout(config.OperationTimeout); err != nil {
		return fmt.Errorf("validate USB operation timeout: %w", err)
	}

	return nil
}
