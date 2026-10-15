// Core Audio device rates and exclusive access, for coreaudio_darwin.go.

#import <CoreAudio/CoreAudio.h>
#import <Foundation/Foundation.h>

extern void goAudioDevicesChanged(void);

// deviceFor finds the device with uid, or the default output for "".
static AudioObjectID deviceFor(const char *uid) {
	AudioObjectID dev = kAudioObjectUnknown;
	UInt32 size = sizeof(dev);
	if (uid == NULL || uid[0] == 0) {
		AudioObjectPropertyAddress addr = {kAudioHardwarePropertyDefaultOutputDevice,
			kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
		AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &size, &dev);
		return dev;
	}
	CFStringRef s = CFStringCreateWithCString(NULL, uid, kCFStringEncodingUTF8);
	AudioValueTranslation t = {&s, sizeof(s), &dev, sizeof(dev)};
	size = sizeof(t);
	AudioObjectPropertyAddress addr = {kAudioHardwarePropertyDeviceForUID,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &size, &t);
	CFRelease(s);
	return dev;
}

static AudioObjectPropertyAddress rateAddr = {kAudioDevicePropertyNominalSampleRate,
	kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};

double caNominalRate(const char *uid) {
	AudioObjectID dev = deviceFor(uid);
	if (dev == kAudioObjectUnknown) return 0;
	Float64 rate = 0;
	UInt32 size = sizeof(rate);
	if (AudioObjectGetPropertyData(dev, &rateAddr, 0, NULL, &size, &rate) != noErr) return 0;
	return rate;
}

// caSetNominalRate returns 0 once the device runs at rate, -1 if it can't.
int caSetNominalRate(const char *uid, double rate) {
	AudioObjectID dev = deviceFor(uid);
	if (dev == kAudioObjectUnknown) return -1;

	// Only ask for a rate the device lists.
	AudioObjectPropertyAddress availAddr = {kAudioDevicePropertyAvailableNominalSampleRates,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(dev, &availAddr, 0, NULL, &size) != noErr || size == 0) return -1;
	UInt32 count = size / sizeof(AudioValueRange);
	AudioValueRange *ranges = malloc(size);
	AudioObjectGetPropertyData(dev, &availAddr, 0, NULL, &size, ranges);
	bool supported = false;
	for (UInt32 i = 0; i < count; i++) {
		if (rate >= ranges[i].mMinimum && rate <= ranges[i].mMaximum) supported = true;
	}
	free(ranges);
	if (!supported) return -1;

	Float64 current = caNominalRate(uid);
	if (current == rate) return 0;
	Float64 want = rate;
	if (AudioObjectSetPropertyData(dev, &rateAddr, 0, NULL, sizeof(want), &want) != noErr) return -1;
	// The change happens asynchronously; wait up to a second for it.
	for (int i = 0; i < 100; i++) {
		if (caNominalRate(uid) == rate) return 0;
		[NSThread sleepForTimeInterval:0.01];
	}
	return -1;
}

void caWatchDevices(void) {
	AudioObjectPropertyAddress addrs[2] = {
		{kAudioHardwarePropertyDevices, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain},
		{kAudioHardwarePropertyDefaultOutputDevice, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain},
	};
	for (int i = 0; i < 2; i++) {
		AudioObjectAddPropertyListenerBlock(kAudioObjectSystemObject, &addrs[i],
			dispatch_get_main_queue(), ^(UInt32 n, const AudioObjectPropertyAddress *a) {
				goAudioDevicesChanged();
			});
	}
}

static AudioObjectPropertyAddress hogAddr = {kAudioDevicePropertyHogMode,
	kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};

// hogOwner is the process with the device to itself, or -1 for none.
static pid_t hogOwner(AudioObjectID dev) {
	pid_t owner = -1;
	UInt32 size = sizeof(owner);
	if (AudioObjectGetPropertyData(dev, &hogAddr, 0, NULL, &size, &owner) != noErr) return -1;
	return owner;
}

// caHog takes the device with uid, or the default output for "", for this
// process alone. It returns the device, or 0 if another process has it or
// it can't be had. Setting the property toggles it, whatever the value.
unsigned int caHog(const char *uid) {
	AudioObjectID dev = deviceFor(uid);
	if (dev == kAudioObjectUnknown) return 0;
	pid_t me = getpid();
	pid_t owner = hogOwner(dev);
	if (owner == me) return dev;
	if (owner != -1) return 0;
	Boolean settable = false;
	if (AudioObjectIsPropertySettable(dev, &hogAddr, &settable) != noErr || !settable) return 0;
	pid_t v = me;
	if (AudioObjectSetPropertyData(dev, &hogAddr, 0, NULL, sizeof(v), &v) != noErr) return 0;
	return hogOwner(dev) == me ? dev : 0;
}

// caUnhog gives the device back to every process, if this one has it.
void caUnhog(unsigned int dev) {
	if (dev == 0 || hogOwner(dev) != getpid()) return;
	pid_t v = -1;
	AudioObjectSetPropertyData(dev, &hogAddr, 0, NULL, sizeof(v), &v);
}

// caHogOwner is the process that has the device to itself, or -1.
int caHogOwner(unsigned int dev) {
	return dev == 0 ? -1 : hogOwner(dev);
}
