package packet

import "fmt"

// WrongDeviceAttachmentError error type that volume is attached to a different device
type WrongDeviceAttachmentError struct {
	deviceID string
}

// Error return the error string
func (w WrongDeviceAttachmentError) Error() string {
	return fmt.Sprintf("Attached to wrong device: %s", w.deviceID)
}

// IsWrongDeviceAttachment check if this error is a wrong device attachment
func IsWrongDeviceAttachment(err error) bool {
	switch err.(type) {
	case *WrongDeviceAttachmentError:
		return true
	}
	return false
}

// TooManyDevicesAttachedError error type that volume is attached to multiple devices
type TooManyDevicesAttachedError struct {
	deviceIDs []string
}

// Error return the error string
func (t TooManyDevicesAttachedError) Error() string {
	return fmt.Sprintf("Attached to multiple devices: %v", t.deviceIDs)
}

// IsTooManyDevicesAttached check if this error is a too many devices attached error
func IsTooManyDevicesAttached(err error) bool {
	switch err.(type) {
	case *TooManyDevicesAttachedError:
		return true
	}
	return false
}
