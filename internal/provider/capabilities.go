package provider

type Capabilities struct {
	Streaming        bool `json:"streaming"`
	StructuredJSON   bool `json:"structured_json"`
	ListModels       bool `json:"list_models"`
	ToolCalling      bool `json:"tool_calling"`
	Vision           bool `json:"vision"`
	MaxInputChars    int `json:"max_input_chars"`
	MaxOutputTokens  int `json:"max_output_tokens"`
	JSONOutputTokens int `json:"json_output_tokens"`
}

type CapabilityProvider interface {
	Capabilities() Capabilities
}

func CapabilitiesOf(p Provider) Capabilities {
	if capable, ok := p.(CapabilityProvider); ok {
		return capable.Capabilities()
	}
	return Capabilities{
		Streaming:      true,
		StructuredJSON: true,
		ListModels:     true,
	}
}

func SupportsRequest(cap Capabilities, opts RequestOptions) bool {
	if opts.JSONMode && !cap.StructuredJSON {
		return false
	}
	return true
}
