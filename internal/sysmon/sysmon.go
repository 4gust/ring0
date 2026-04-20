package sysmon

import (
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Snapshot is a point-in-time system metrics reading.
type Snapshot struct {
	CPUPercent  float64
	MemPercent  float64
	MemUsedMB   uint64
	MemTotalMB  uint64
}

// Sample returns current CPU + memory usage.
func Sample() Snapshot {
	s := Snapshot{}
	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		s.CPUPercent = pcts[0]
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemPercent = vm.UsedPercent
		s.MemUsedMB = vm.Used / 1024 / 1024
		s.MemTotalMB = vm.Total / 1024 / 1024
	}
	return s
}
