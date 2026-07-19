//go:build darwin && cgo

#import <Foundation/Foundation.h>

void *keynopeStartAccessingBookmark(const void *bytes, size_t length) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:bytes length:length];
        BOOL stale = NO;
        NSError *error = nil;
        NSURL *url = [NSURL URLByResolvingBookmarkData:data
                                               options:0
                                         relativeToURL:nil
                                   bookmarkDataIsStale:&stale
                                                 error:&error];
        // Resolving an inter-process bookmark implicitly extends the helper's
        // sandbox. It is not an app-scoped bookmark and must use options:0.
        if (url == nil) {
            return NULL;
        }
        [url retain];
        return (void *)url;
    }
}

void keynopeStopAccessingBookmark(void *handle) {
    if (handle == NULL) {
        return;
    }
    @autoreleasepool {
        NSURL *url = (NSURL *)handle;
        [url stopAccessingSecurityScopedResource];
        [url release];
    }
}
