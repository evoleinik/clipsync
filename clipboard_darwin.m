#import <AppKit/AppKit.h>
#import <stdlib.h>
#import <string.h>

int clipChangeCount() {
    return (int)[[NSPasteboard generalPasteboard] changeCount];
}

// Returns: 0=empty, 'T'=text, 'I'=image
// Caller must free *outData with clipFree()
int clipRead(void** outData, int* outLen) {
    *outData = NULL;
    *outLen = 0;
    @autoreleasepool {
        NSPasteboard* pb = [NSPasteboard generalPasteboard];

        // PNG first
        NSData* png = [pb dataForType:NSPasteboardTypePNG];
        if (png != nil && [png length] > 0) {
            *outLen = (int)[png length];
            *outData = malloc(*outLen);
            memcpy(*outData, [png bytes], *outLen);
            return 'I';
        }

        // TIFF (macOS screenshots) — convert to PNG
        NSData* tiff = [pb dataForType:NSPasteboardTypeTIFF];
        if (tiff != nil && [tiff length] > 0) {
            NSBitmapImageRep* rep = [NSBitmapImageRep imageRepWithData:tiff];
            if (rep != nil) {
                NSData* pngData = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
                if (pngData != nil && [pngData length] > 0) {
                    *outLen = (int)[pngData length];
                    *outData = malloc(*outLen);
                    memcpy(*outData, [pngData bytes], *outLen);
                    return 'I';
                }
            }
        }

        // Plain text
        NSString* str = [pb stringForType:NSPasteboardTypeString];
        if (str != nil && [str length] > 0) {
            const char* utf8 = [str UTF8String];
            int slen = (int)strlen(utf8);
            *outData = malloc(slen);
            memcpy(*outData, utf8, slen);
            *outLen = slen;
            return 'T';
        }

        return 0;
    }
}

void clipWriteText(const void* data, int len) {
    @autoreleasepool {
        NSPasteboard* pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        NSString* str = [[NSString alloc] initWithBytes:data length:len encoding:NSUTF8StringEncoding];
        if (str != nil) {
            [pb setString:str forType:NSPasteboardTypeString];
        }
    }
}

void clipWriteImage(const void* data, int len) {
    @autoreleasepool {
        NSPasteboard* pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        NSData* imgData = [NSData dataWithBytes:data length:len];
        [pb setData:imgData forType:NSPasteboardTypePNG];
    }
}

void clipFree(void* data) {
    free(data);
}
