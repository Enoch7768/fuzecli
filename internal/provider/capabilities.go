package provider

type Capabilities struct {
	Streaming       bool
	StructuredJSON  bool
	ListModels      bool
	ToolCalling     bool
	Vision          bool
	MaxInputChars   int
	MaxOutputTokens int
	JSONOutputTokens int
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
		ListModels:     true,
	}
}
