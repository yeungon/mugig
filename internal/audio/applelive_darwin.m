// Decoding a stream as it arrives with macOS's own decoders, for
// applelive_darwin.go: AudioFileStream finds the packets in the bytes fed
// to it, and an AudioConverter decodes them.

#import <AudioToolbox/AudioToolbox.h>
#include <stdlib.h>
#include <string.h>

// needMore is returned by supply when every packet has been decoded, so
// the converter stops and waits for more to be fed.
static const OSStatus needMore = 'more';

// maxBadPackets is how many packets in a row can fail to decode before
// the stream is given up on. A packet or two can be damaged on the way.
static const int maxBadPackets = 50;

typedef struct {
	AudioFileStreamID parser;
	AudioStreamBasicDescription in; // the stream's format, once known
	int ready;                      // the format is known
	int failed;                     // the format can't be decoded
	AudioConverterRef conv;
	UInt32 channels; // decoded
	void *cookie;    // the decoder's configuration, if the stream has one
	UInt32 cookieSize;

	// Packets found and not yet decoded, one after another.
	unsigned char *data;
	size_t head, len, cap; // the first packet's start, the end, the room
	UInt32 *sizes;         // each packet's length
	size_t first, count, sizesCap;

	// The packet the converter is decoding, kept until it asks for the next.
	unsigned char *cur;
	size_t curCap;
	AudioStreamPacketDescription curDesc;
	int bad; // packets in a row that failed to decode
} liveDecoder;

// chooseFormat takes the best format the stream can be decoded as. An
// ADTS stream says it is plain AAC at half the rate even when it is
// HE-AAC; the format list has the HE-AAC reading first.
static void chooseFormat(liveDecoder *d) {
	AudioStreamBasicDescription f = {0};
	UInt32 size = 0;
	Boolean writable;
	if (AudioFileStreamGetPropertyInfo(d->parser, kAudioFileStreamProperty_FormatList, &size, &writable) == noErr &&
		size >= sizeof(AudioFormatListItem)) {
		AudioFormatListItem *list = malloc(size);
		if (AudioFileStreamGetProperty(d->parser, kAudioFileStreamProperty_FormatList, &size, list) == noErr) {
			UInt32 i = 0, isize = sizeof(i);
			if (AudioFormatGetProperty(kAudioFormatProperty_FirstPlayableFormatFromList, size, list, &isize, &i) != noErr) {
				i = 0;
			}
			f = list[i].mASBD;
		}
		free(list);
	}
	if (f.mFormatID == 0) {
		size = sizeof(f);
		AudioFileStreamGetProperty(d->parser, kAudioFileStreamProperty_DataFormat, &size, &f);
	}
	d->in = f;
	if (AudioFileStreamGetPropertyInfo(d->parser, kAudioFileStreamProperty_MagicCookieData, &size, &writable) == noErr &&
		size > 0) {
		d->cookie = malloc(size);
		d->cookieSize = size;
		if (AudioFileStreamGetProperty(d->parser, kAudioFileStreamProperty_MagicCookieData, &size, d->cookie) != noErr) {
			free(d->cookie);
			d->cookie = NULL;
			d->cookieSize = 0;
		}
	}

	AudioStreamBasicDescription out = {0};
	d->channels = f.mChannelsPerFrame == 1 ? 1 : 2; // more are mixed into two
	out.mSampleRate = f.mSampleRate;
	out.mFormatID = kAudioFormatLinearPCM;
	out.mFormatFlags = kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked;
	out.mChannelsPerFrame = d->channels;
	out.mBitsPerChannel = 32;
	out.mBytesPerFrame = 4 * d->channels;
	out.mFramesPerPacket = 1;
	out.mBytesPerPacket = out.mBytesPerFrame;
	if (f.mSampleRate <= 0 || AudioConverterNew(&d->in, &out, &d->conv) != noErr) {
		d->failed = 1;
		return;
	}
	if (d->cookie != NULL) {
		AudioConverterSetProperty(d->conv, kAudioConverterDecompressionMagicCookie, d->cookieSize, d->cookie);
	}
	d->ready = 1;
}

static void onProperty(void *ctx, AudioFileStreamID s, AudioFileStreamPropertyID id, AudioFileStreamPropertyFlags *flags) {
	liveDecoder *d = ctx;
	if (id == kAudioFileStreamProperty_ReadyToProducePackets && !d->ready && !d->failed) {
		chooseFormat(d);
	}
}

static int grow(void **p, size_t *cap, size_t need, size_t unit) {
	if (need <= *cap) return 1;
	size_t c = *cap ? *cap : 64;
	while (c < need) c *= 2;
	void *q = realloc(*p, c * unit);
	if (q == NULL) return 0;
	*p = q;
	*cap = c;
	return 1;
}

// onPackets keeps the packets found, to be decoded.
static void onPackets(void *ctx, UInt32 nBytes, UInt32 nPackets, const void *data, AudioStreamPacketDescription *descs) {
	liveDecoder *d = ctx;
	// Move what is left to the front before adding more.
	if (d->head > 0) {
		memmove(d->data, d->data + d->head, d->len - d->head);
		d->len -= d->head;
		d->head = 0;
	}
	if (d->first > 0) {
		memmove(d->sizes, d->sizes + d->first, d->count * sizeof(UInt32));
		d->first = 0;
	}
	for (UInt32 i = 0; i < nPackets; i++) {
		SInt64 start = descs ? descs[i].mStartOffset : (SInt64)i * d->in.mBytesPerPacket;
		UInt32 size = descs ? descs[i].mDataByteSize : d->in.mBytesPerPacket;
		if (size == 0 || start < 0 || start + size > nBytes) continue;
		if (!grow((void **)&d->data, &d->cap, d->len + size, 1) ||
			!grow((void **)&d->sizes, &d->sizesCap, d->count + 1, sizeof(UInt32))) {
			return;
		}
		memcpy(d->data + d->len, (const unsigned char *)data + start, size);
		d->len += size;
		d->sizes[d->count++] = size;
	}
}

// supply hands the converter the next packet.
static OSStatus supply(AudioConverterRef conv, UInt32 *n, AudioBufferList *io, AudioStreamPacketDescription **descs, void *ctx) {
	liveDecoder *d = ctx;
	if (d->count == 0) {
		*n = 0;
		return needMore;
	}
	UInt32 size = d->sizes[d->first];
	if (!grow((void **)&d->cur, &d->curCap, size, 1)) {
		*n = 0;
		return needMore;
	}
	memcpy(d->cur, d->data + d->head, size);
	d->head += size;
	d->first++;
	d->count--;

	io->mNumberBuffers = 1;
	io->mBuffers[0].mData = d->cur;
	io->mBuffers[0].mDataByteSize = size;
	io->mBuffers[0].mNumberChannels = d->in.mChannelsPerFrame;
	d->curDesc.mStartOffset = 0;
	d->curDesc.mVariableFramesInPacket = 0;
	d->curDesc.mDataByteSize = size;
	if (descs != NULL) *descs = &d->curDesc;
	*n = 1;
	return noErr;
}

// liveOpen starts a decoder for a stream of the kind hint names, such as
// kAudioFileAAC_ADTSType.
void *liveOpen(unsigned int hint) {
	liveDecoder *d = calloc(1, sizeof(liveDecoder));
	if (AudioFileStreamOpen(d, onProperty, onPackets, hint, &d->parser) != noErr) {
		free(d);
		return NULL;
	}
	return d;
}

// liveFeed gives the decoder the stream's next bytes. It returns 0, or -1
// if they can't be read as the stream they should be.
int liveFeed(void *h, const void *data, int n) {
	liveDecoder *d = h;
	OSStatus st = AudioFileStreamParseBytes(d->parser, n, data, 0);
	return st == noErr && !d->failed ? 0 : -1;
}

// liveFormat fills in the decoded sound's rate and channels, returning 1
// once they are known.
int liveFormat(void *h, double *rate, int *channels) {
	liveDecoder *d = h;
	if (!d->ready) return 0;
	*rate = d->in.mSampleRate;
	*channels = d->channels;
	return 1;
}

// liveDecode decodes up to frames frames into out, interleaved floats,
// returning how many: 0 when it needs more bytes fed, -1 if the stream
// can't be decoded.
int liveDecode(void *h, float *out, int frames) {
	liveDecoder *d = h;
	if (!d->ready) return 0;
	for (;;) {
		AudioBufferList list;
		list.mNumberBuffers = 1;
		list.mBuffers[0].mNumberChannels = d->channels;
		list.mBuffers[0].mDataByteSize = frames * 4 * d->channels;
		list.mBuffers[0].mData = out;
		UInt32 n = frames;
		OSStatus st = AudioConverterFillComplexBuffer(d->conv, supply, d, &n, &list, NULL);
		if (st == noErr || st == needMore) {
			if (n > 0) d->bad = 0;
			return (int)n;
		}
		// A damaged packet: start afresh from the next.
		if (++d->bad > maxBadPackets) return -1;
		AudioConverterReset(d->conv);
		if (n > 0) return (int)n;
	}
}

void liveClose(void *h) {
	liveDecoder *d = h;
	if (d->conv != NULL) AudioConverterDispose(d->conv);
	AudioFileStreamClose(d->parser);
	free(d->cookie);
	free(d->data);
	free(d->sizes);
	free(d->cur);
	free(d);
}
