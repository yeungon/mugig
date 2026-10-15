// The Dock icon, for dock_darwin.go.

#import <Cocoa/Cocoa.h>

void dockSetIcon(const void *png, int length) {
	if ([[NSBundle mainBundle] bundleIdentifier] != nil) {
		return; // in the app bundle, which has its own icon
	}
	NSData *data = [NSData dataWithBytes:png length:length];
	NSImage *icon = [[NSImage alloc] initWithData:data];
	if (icon == nil) {
		return;
	}
	// Apple's icons fill 824 of 1024 points, centred; the rounded icon's
	// shape fills 94% of its picture.
	const CGFloat side = 1024, shape = 824, drawn = shape / 0.94;
	NSImage *framed = [NSImage imageWithSize:NSMakeSize(side, side) flipped:NO
		drawingHandler:^BOOL(NSRect rect) {
			CGFloat at = (side - drawn) / 2;
			[icon drawInRect:NSMakeRect(at, at, drawn, drawn)];
			return YES;
		}];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSApp.applicationIconImage = framed;
	});
}
