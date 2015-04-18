package proctl

import (
	"fmt"
	"runtime"
)

// Represents a single breakpoint. Stores information on the break
// point including the byte of data that originally was stored at that
// address.
type BreakPoint struct {
	FunctionName string
	File         string
	Line         int
	Addr         uint64
	OriginalData []byte
	ID           int
	Temp         bool
	hardware     bool
	reg          int
}

func (bp *BreakPoint) String() string {
	return fmt.Sprintf("Breakpoint %d at %#v %s:%d", bp.ID, bp.Addr, bp.File, bp.Line)
}

func (bp *BreakPoint) Clear(thread *ThreadContext) (*BreakPoint, error) {
	if bp.hardware {
		if err := clearHardwareBreakpoint(bp.reg, thread.Id); err != nil {
			return nil, err
		}
		return bp, nil
	}
	if _, err := writeMemory(thread, uintptr(bp.Addr), bp.OriginalData); err != nil {
		return nil, fmt.Errorf("could not clear breakpoint %s", err)
	}
	return bp, nil
}

// Returned when trying to set a breakpoint at
// an address that already has a breakpoint set for it.
type BreakPointExistsError struct {
	file string
	line int
	addr uint64
}

func (bpe BreakPointExistsError) Error() string {
	return fmt.Sprintf("Breakpoint exists at %s:%d at %x", bpe.file, bpe.line, bpe.addr)
}

// InvalidAddressError represents the result of
// attempting to set a breakpoint at an invalid address.
type InvalidAddressError struct {
	address uint64
}

func (iae InvalidAddressError) Error() string {
	return fmt.Sprintf("Invalid address %#v\n", iae.address)
}

// Returns whether or not a breakpoint has been set for the given address.
func (dbp *DebuggedProcess) BreakpointExists(addr uint64) bool {
	for _, bp := range dbp.HWBreakPoints {
		// TODO(darwin)
		if runtime.GOOS == "darwin" {
			break
		}
		if bp != nil && bp.Addr == addr {
			return true
		}
	}
	_, ok := dbp.BreakPoints[addr]
	return ok
}

func (dbp *DebuggedProcess) newBreakpoint(fn, f string, l int, addr uint64, data []byte, temp bool) *BreakPoint {
	dbp.breakpointIDCounter++
	return &BreakPoint{
		FunctionName: fn,
		File:         f,
		Line:         l,
		Addr:         addr,
		OriginalData: data,
		ID:           dbp.breakpointIDCounter,
		Temp:         temp,
	}
}

func (dbp *DebuggedProcess) newHardwareBreakpoint(fn, f string, l int, addr uint64, data []byte, temp bool, reg int) *BreakPoint {
	bp := dbp.newBreakpoint(fn, f, l, addr, data, temp)
	bp.hardware = true
	bp.reg = reg
	return bp
}

func (dbp *DebuggedProcess) setBreakpoint(tid int, addr uint64, temp bool) (*BreakPoint, error) {
	var f, l, fn = dbp.goSymTable.PCToLine(uint64(addr))
	if fn == nil {
		return nil, InvalidAddressError{address: addr}
	}
	if dbp.BreakpointExists(addr) {
		return nil, BreakPointExistsError{f, l, addr}
	}
	// Try and set a hardware breakpoint.
	for i, v := range dbp.HWBreakPoints {
		// TODO(darwin)
		if runtime.GOOS == "darwin" {
			break
		}
		if v == nil {
			if err := setHardwareBreakpoint(i, tid, addr); err != nil {
				return nil, fmt.Errorf("could not set hardware breakpoint: %v", err)
			}
			dbp.HWBreakPoints[i] = dbp.newHardwareBreakpoint(fn.Name, f, l, addr, nil, temp, i)
			return dbp.HWBreakPoints[i], nil
		}
	}
	// Fall back to software breakpoint. 0xCC is INT 3, software
	// breakpoint trap interrupt.
	thread := dbp.Threads[tid]
	originalData := make([]byte, 1)
	if _, err := readMemory(thread, uintptr(addr), originalData); err != nil {
		return nil, err
	}
	if _, err := writeMemory(thread, uintptr(addr), []byte{0xCC}); err != nil {
		return nil, err
	}
	dbp.BreakPoints[addr] = dbp.newBreakpoint(fn.Name, f, l, addr, originalData, temp)
	return dbp.BreakPoints[addr], nil
}

func (dbp *DebuggedProcess) clearBreakpoint(tid int, addr uint64) (*BreakPoint, error) {
	thread := dbp.Threads[tid]
	// Check for hardware breakpoint
	for i, bp := range dbp.HWBreakPoints {
		if bp == nil {
			continue
		}
		if bp.Addr == addr {
			_, err := bp.Clear(thread)
			if err != nil {
				return nil, err
			}
			dbp.HWBreakPoints[i] = nil
			return bp, nil
		}
	}
	// Check for software breakpoint
	if bp, ok := dbp.BreakPoints[addr]; ok {
		if _, err := bp.Clear(thread); err != nil {
			return nil, err
		}
		delete(dbp.BreakPoints, addr)
		return bp, nil
	}
	return nil, fmt.Errorf("no breakpoint at %#v", addr)
}
