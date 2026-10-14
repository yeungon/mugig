// Decoding with macOS's own decoders (ExtAudioFile), for apple_darwin.go.

#import <AudioToolbox/AudioToolbox.h>
#import <Foundation/Foundation.h>

// appleOpen opens path for reading as interleaved 32-bit samples: floats
// if asFloat, else signed integers. It fills in the file's own format and
// returns NULL on failure.
void *appleOpen(const char *path, int asFloat, double *rate, int *channels, int *bits, long long *frames) {
	CFURLRef url = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path, strlen(path), false);
	ExtAudioFileRef f = NULL;
	OSStatus st = ExtAudioFileOpenURL(url, &f);
	CFRelease(url);
	if (st != noErr) return NULL;

	AudioStreamBasicDescription src = {0};
	UInt32 size = sizeof(src);
	ExtAudioFileGetProperty(f, kExtAudioFileProperty_FileDataFormat, &size, &src);
	SInt64 n = 0;
	size = sizeof(n);
	ExtAudioFileGetProperty(f, kExtAudioFileProperty_FileLengthFrames, &size, &n);
	*rate = src.mSampleRate;
	*channels = (int)src.mChannelsPerFrame;
	// Lossless codecs keep the source's bit depth in mFormatFlags.
	int b = 0;
	if (src.mFormatID == kAudioFormatFLAC || src.mFormatID == kAudioFormatAppleLossless) {
		switch (src.mFormatFlags) {
		case kAppleLosslessFormatFlag_16BitSourceData: b = 16; break;
		case kAppleLosslessFormatFlag_20BitSourceData: b = 20; break;
		case kAppleLosslessFormatFlag_24BitSourceData: b = 24; break;
		case kAppleLosslessFormatFlag_32BitSourceData: b = 32; break;
		}
	} else {
		b = (int)src.mBitsPerChannel;
	}
	*bits = b;
	*frames = n;

	AudioStreamBasicDescription client = {0};
	client.mSampleRate = src.mSampleRate;
	client.mFormatID = kAudioFormatLinearPCM;
	client.mFormatFlags = (asFloat ? kAudioFormatFlagIsFloat : kAudioFormatFlagIsSignedInteger) | kAudioFormatFlagIsPacked;
	client.mChannelsPerFrame = src.mChannelsPerFrame;
	client.mBitsPerChannel = 32;
	client.mBytesPerFrame = 4 * src.mChannelsPerFrame;
	client.mFramesPerPacket = 1;
	client.mBytesPerPacket = client.mBytesPerFrame;
	if (ExtAudioFileSetProperty(f, kExtAudioFileProperty_ClientDataFormat, sizeof(client), &client) != noErr) {
		ExtAudioFileDispose(f);
		return NULL;
	}
	return f;
}

// appleRead reads up to frames frames into buf, returning how many were
// read: 0 at the end, -1 on error.
int appleRead(void *h, void *buf, int frames, int channels) {
	AudioBufferList list;
	list.mNumberBuffers = 1;
	list.mBuffers[0].mNumberChannels = channels;
	list.mBuffers[0].mDataByteSize = frames * 4 * channels;
	list.mBuffers[0].mData = buf;
	UInt32 n = frames;
	if (ExtAudioFileRead((ExtAudioFileRef)h, &n, &list) != noErr) return -1;
	return (int)n;
}

int appleSeek(void *h, long long frame) {
	return ExtAudioFileSeek((ExtAudioFileRef)h, frame) == noErr ? 0 : -1;
}

void appleClose(void *h) {
	ExtAudioFileDispose((ExtAudioFileRef)h);
}
