package runner

type SupportedFeatures struct {
	SplitByFile     bool
	SplitByExample  bool
	FilterTestFiles bool
	FilterTestByTag bool
	AutoRetry       bool
	Mute            bool
	Skip            bool
}
