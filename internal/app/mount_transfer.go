package app

import "context"

// A FUSE callback context belongs to one request and its cancel channel can be
// reused. Keep file-handle transfers on the service lifetime instead.
func (d mountFS) beginTransfer(kind string, storage Storage, name string, total int64, locations ...transferLocations) *transferProgress {
	ctx := context.WithValue(d.app.ctx, transferSourceKey{}, "FUSE")
	return d.app.beginTransfer(ctx, kind, storage, name, total, locations...)
}

// Only the active operation follows the callback cancellation. Transfer deletion
// and service shutdown still cancel upstream publication immediately.
func (d mountFS) transferContext(request context.Context, progress *transferProgress) (context.Context, func()) {
	base := d.app.ctx
	if progress != nil {
		base = progress.ctx
	}
	ctx, cancel := context.WithCancel(base)
	stop := context.AfterFunc(request, cancel)
	if request.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
