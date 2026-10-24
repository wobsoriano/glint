package platform

var (
	PerMonitorV2 = perMonitorV2
	RunCommand   = runCommand
	WindowScale  = windowScale
)

// SetPerMonitorV2 repeats the call MyGo's Backend.Init makes.
func SetPerMonitorV2() error {
	_, err := call(procSetProcessDpiAwarenessContext, dpiAwarenessContextPerMonitorV2)
	return err
}

const RunKey, RunValue = runKey, runValue
