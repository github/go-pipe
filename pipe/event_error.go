package pipe

// Event represents anything that could happen during the pipeline execution
type Event struct {
	Command string
	Msg     string
	Err     error
	Context map[string]interface{}
}
