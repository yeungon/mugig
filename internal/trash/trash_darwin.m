#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

// trashMove moves the file to the Trash and returns NULL, or returns why
// it couldn't, which the caller frees.
char *trashMove(const char *path) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		NSError *err = nil;
		if ([[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:nil error:&err]) {
			return NULL;
		}
		return strdup(err.localizedDescription.UTF8String ?: "unknown error");
	}
}
