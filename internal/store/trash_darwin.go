package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var loadFoundation = sync.OnceValue(func() error {
	_, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	return err
})

// trash uses NSFileManager, as the Rust build did, so Finder's Put Back can restore a capture.
func trash(paths []string) error {
	if err := loadFoundation(); err != nil {
		return fmt.Errorf("loading Foundation: %w", err)
	}
	sel := objc.RegisterName
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(sel("new"))
	defer pool.Send(sel("drain"))
	files := objc.ID(objc.GetClass("NSFileManager")).Send(sel("defaultManager"))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		str := objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), abs)
		url := objc.ID(objc.GetClass("NSURL")).Send(sel("fileURLWithPath:"), str)
		var nserr objc.ID
		if !objc.Send[bool](files, sel("trashItemAtURL:resultingItemURL:error:"), url, objc.ID(0), &nserr) {
			if nserr == 0 {
				return fmt.Errorf("moving %s to the Trash failed", abs)
			}
			desc := nserr.Send(sel("localizedDescription"))
			return fmt.Errorf("moving %s to the Trash: %w", abs, errors.New(objc.Send[string](desc, sel("UTF8String"))))
		}
	}
	return nil
}
