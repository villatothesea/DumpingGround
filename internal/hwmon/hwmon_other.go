//go:build !windows

package hwmon

type platform struct{}

func (p *platform) load(info *Info) {}

func (p *platform) enumNICs() []NICInfo { return nil }

func (p *platform) sample(s *Sample, info *Info, heavy bool) {}
