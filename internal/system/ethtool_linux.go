package system

import (
	"encoding/binary"
	"errors"
	"syscall"
	"unsafe"
)

// The link modes of an Ethernet interface, read the way ethtool reads them: SIOCETHTOOL with
// ETHTOOL_GLINKSETTINGS on any socket. Reading needs no privilege, so the daemon asks itself;
// nothing here sets anything (the speed limit is a .link file udev applies, ethspeed.go).

const (
	siocEthtool          = 0x8946
	ethtoolGLinkSettings = 0x4c
	// struct ethtool_link_settings up to link_mode_masks: cmd, speed, eight bytes that end in
	// link_mode_masks_nwords, four more bytes, seven reserved words
	ethtoolLinkHeader = 48
	ethtoolNWordsAt   = 15
)

// ifreqData is struct ifreq with ifr_data: the name, the pointer, padding up to the 64-bit size.
type ifreqData struct {
	name [16]byte
	data unsafe.Pointer
	_    [16]byte
}

// readLinkModes asks the kernel for the interface's supported and advertised link modes, each a
// bitmap in 32-bit words (ETHTOOL_LINK_MODE_*_BIT). The first call learns how many words the
// kernel has (it answers the negative count), the second reads them.
func readLinkModes(name string) (supported, advertising []uint32, err error) {
	if len(name) == 0 || len(name) > 15 {
		return nil, nil, errors.New("interface name")
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	defer syscall.Close(fd)
	call := func(buf []byte) error {
		var ifr ifreqData
		copy(ifr.name[:], name)
		ifr.data = unsafe.Pointer(&buf[0])
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), siocEthtool, uintptr(unsafe.Pointer(&ifr))); e != 0 {
			return e
		}
		return nil
	}
	probe := make([]byte, ethtoolLinkHeader)
	binary.NativeEndian.PutUint32(probe, ethtoolGLinkSettings)
	if err := call(probe); err != nil {
		return nil, nil, err
	}
	n := -int(int8(probe[ethtoolNWordsAt]))
	if n <= 0 || n > 64 {
		return nil, nil, errors.New("no link mode words")
	}
	buf := make([]byte, ethtoolLinkHeader+3*4*n)
	binary.NativeEndian.PutUint32(buf, ethtoolGLinkSettings)
	buf[ethtoolNWordsAt] = byte(int8(n))
	if err := call(buf); err != nil {
		return nil, nil, err
	}
	words := func(i int) []uint32 {
		out := make([]uint32, n)
		for w := range out {
			out[w] = binary.NativeEndian.Uint32(buf[ethtoolLinkHeader+4*(i*n+w):])
		}
		return out
	}
	return words(0), words(1), nil
}
