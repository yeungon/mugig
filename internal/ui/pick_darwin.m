// The macOS Open and Save panels, for pick_darwin.go.

#import <Cocoa/Cocoa.h>
#include <stdbool.h>
#include <stdint.h>

extern void goPickDone(int id, char *paths);

void pickShow(uintptr_t window, int id, bool files, bool folders, bool multiple,
	const char *message, const char *start) {
	NSString *msg = [NSString stringWithUTF8String:message];
	NSString *dir = [NSString stringWithUTF8String:start];
	// Run after the current event, outside Fyne's callback.
	dispatch_async(dispatch_get_main_queue(), ^{
		NSOpenPanel *panel = [NSOpenPanel openPanel];
		panel.canChooseFiles = files;
		panel.canChooseDirectories = folders;
		panel.allowsMultipleSelection = multiple;
		panel.message = msg;
		panel.prompt = @"Open";
		if (files) {
			// Folders stay choosable when allowed; files are limited to audio.
			#pragma clang diagnostic push
			#pragma clang diagnostic ignored "-Wdeprecated-declarations"
			panel.allowedFileTypes = @[ @"mp3", @"flac", @"wav", @"ogg" ];
			#pragma clang diagnostic pop
		}
		if (dir.length > 0) {
			panel.directoryURL = [NSURL fileURLWithPath:dir isDirectory:YES];
		}
		void (^done)(NSModalResponse) = ^(NSModalResponse result) {
			if (result != NSModalResponseOK) {
				goPickDone(id, NULL);
				return;
			}
			NSMutableArray<NSString *> *paths = [NSMutableArray array];
			for (NSURL *url in panel.URLs) {
				[paths addObject:url.path];
			}
			goPickDone(id, (char *)[[paths componentsJoinedByString:@"\n"] UTF8String]);
		};
		NSWindow *win = (__bridge NSWindow *)(void *)window;
		if (win != nil) {
			[panel beginSheetModalForWindow:win completionHandler:done];
		} else {
			[panel beginWithCompletionHandler:done];
		}
	});
}

void saveShow(uintptr_t window, int id, const char *name, const char *type) {
	NSString *file = [NSString stringWithUTF8String:name];
	NSString *ext = [NSString stringWithUTF8String:type];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSSavePanel *panel = [NSSavePanel savePanel];
		panel.nameFieldStringValue = file;
		panel.canCreateDirectories = YES;
		if (ext.length > 0) {
			#pragma clang diagnostic push
			#pragma clang diagnostic ignored "-Wdeprecated-declarations"
			panel.allowedFileTypes = @[ ext ];
			#pragma clang diagnostic pop
		}
		void (^done)(NSModalResponse) = ^(NSModalResponse result) {
			if (result != NSModalResponseOK || panel.URL == nil) {
				goPickDone(id, NULL);
				return;
			}
			goPickDone(id, (char *)[panel.URL.path UTF8String]);
		};
		NSWindow *win = (__bridge NSWindow *)(void *)window;
		if (win != nil) {
			[panel beginSheetModalForWindow:win completionHandler:done];
		} else {
			[panel beginWithCompletionHandler:done];
		}
	});
}
