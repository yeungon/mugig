// macOS "Now Playing": receives the media keys and Control Center commands,
// and publishes the current track for Control Center and the lock screen.

#import <AppKit/AppKit.h>
#import <MediaPlayer/MediaPlayer.h>

#include "_cgo_export.h"

// These match remoteCommand in nowplaying.go.
enum { cmdPlay = 1, cmdPause, cmdToggle, cmdNext, cmdPrevious, cmdStop, cmdSeek };

static void handle(MPRemoteCommand *command, int cmd) {
	[command addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *event) {
		goRemoteCommand(cmd, 0);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
}

void npStart(void) {
	MPRemoteCommandCenter *center = [MPRemoteCommandCenter sharedCommandCenter];
	handle(center.playCommand, cmdPlay);
	handle(center.pauseCommand, cmdPause);
	handle(center.togglePlayPauseCommand, cmdToggle);
	handle(center.nextTrackCommand, cmdNext);
	handle(center.previousTrackCommand, cmdPrevious);
	handle(center.stopCommand, cmdStop);
	[center.changePlaybackPositionCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *event) {
		goRemoteCommand(cmdSeek, ((MPChangePlaybackPositionCommandEvent *)event).positionTime);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
}

// state matches playState in nowplaying.go: 0 stopped, 1 playing, 2 paused.
void npUpdate(const char *title, const char *artist, const char *album,
	double duration, double elapsed, double rate, int state, const void *art, int artLen) {
	@autoreleasepool {
		MPNowPlayingInfoCenter *center = [MPNowPlayingInfoCenter defaultCenter];
		if (state == 0) {
			center.nowPlayingInfo = nil;
			center.playbackState = MPNowPlayingPlaybackStateStopped;
			return;
		}

		NSMutableDictionary *info = [NSMutableDictionary dictionary];
		info[MPMediaItemPropertyTitle] = [NSString stringWithUTF8String:title];
		info[MPMediaItemPropertyArtist] = [NSString stringWithUTF8String:artist];
		info[MPMediaItemPropertyAlbumTitle] = [NSString stringWithUTF8String:album];
		info[MPMediaItemPropertyPlaybackDuration] = @(duration);
		info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = @(elapsed);
		// The rate lets the system move the position along at the playing speed.
		info[MPNowPlayingInfoPropertyPlaybackRate] = @(state == 1 ? rate : 0.0);
		if (art != NULL && artLen > 0) {
			NSImage *image = [[NSImage alloc] initWithData:[NSData dataWithBytes:art length:artLen]];
			if (image != nil) {
				info[MPMediaItemPropertyArtwork] = [[MPMediaItemArtwork alloc]
					initWithBoundsSize:image.size
					    requestHandler:^NSImage *(CGSize size) { return image; }];
			}
		}
		center.nowPlayingInfo = info;
		center.playbackState = state == 1 ? MPNowPlayingPlaybackStatePlaying : MPNowPlayingPlaybackStatePaused;
	}
}
