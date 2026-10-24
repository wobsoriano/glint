package input

// ToCapture maps a physical desktop position to screen-master pixels exactly as the input
// track maps every event. A window map reads the window's current origin, so the calling
// thread must be per-monitor DPI aware for it to come back in physical pixels.
func (m *PointerMap) ToCapture(x, y int) Point { return m.toCapture(x, y) }
