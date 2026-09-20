package owoncontrol

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonscpi"
)

// Waveform returns raw device bytes and only metadata whose encoding is verified.
//
// Example: screen CH1 uses two explicit length-prefixed queries.
func (controller *Controller) Waveform(
	ctx context.Context,
	request *owonmodel.WaveformRequest,
) (_result *owonmodel.Waveform, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Controller.Waveform")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Controller.Waveform: %v", _err) }()
	}

	query, err := owonscpi.WaveformQuery(request)
	if err != nil {
		return nil, err
	}
	session, err := controller.sessionForOperation("get OWON waveform")
	if err != nil {
		return nil, err
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer transaction.Close()
	captureStartedAt := time.Now()
	header, err := transaction.Execute(ctx, owonscpi.ScreenHeaderQuery())
	if err != nil {
		return nil, fmt.Errorf("query waveform header: %w", err)
	}
	if err := owonscpi.ValidateScreenTraceHeader(header); err != nil {
		return nil, fmt.Errorf("parse waveform header: %w", err)
	}
	data, err := transaction.Execute(ctx, query)
	capturedAt := time.Now()
	if err != nil {
		return nil, fmt.Errorf("query waveform channel %d: %w", request.Channel, err)
	}
	metadata := &owonmodel.WaveformMetadata{SampleEncoding: "unverified", DataLengthBytes: uint64(len(data))}

	waveform := &owonmodel.Waveform{
		Channel:          request.Channel,
		Data:             data,
		Encoding:         "owon-raw-unverified",
		ScreenHeaderJSON: header,
		CapturedAt:       capturedAt,
		CaptureStartedAt: captureStartedAt,
		Metadata:         metadata,
	}
	trace, err := owonscpi.DecodeScreenTrace(request.Channel, header, data)
	var unsupported *owonscpi.ErrUnsupportedScreenProfile
	switch {
	case err == nil:
		waveform.ScreenTrace = trace
	case errors.As(err, &unsupported):
		waveform.ScreenTraceUnavailableReason = unsupported.Reason
	default:
		return nil, fmt.Errorf("decode waveform channel %d screen trace: %w", request.Channel, err)
	}
	return waveform, nil
}
