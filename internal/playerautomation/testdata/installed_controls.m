#import <AppKit/AppKit.h>
#import <objc/runtime.h>

// Decode the installed nib without opening a window or logging into SzPlayer.
@interface ProfileNibOwner : NSObject
@property NSView *view;
@property NSSlider *progressView, *volumeSlider;
@property NSTextField *leftTimeLabel, *volumeLabel;
@property NSButton *toogleBotton;
@end
@implementation ProfileNibOwner
@end

int main(int argc, const char **argv) { @autoreleasepool {
    if (argc != 5) return 1;
    const char *names[] = {"VideoSlider", "VideoSliderCell", "JMImageButton", "MouseButton"};
    Class parents[] = {NSSlider.class, NSSliderCell.class, NSButton.class, NSButton.class};
    for (int i = 0; i < 4; i++) objc_registerClassPair(objc_allocateClassPair(parents[i], names[i], 0));
    ProfileNibOwner *owner = [ProfileNibOwner new];
    NSNib *nib = [[NSNib alloc] initWithNibData:[NSData dataWithContentsOfFile:@(argv[1])] bundle:nil];
    NSArray *top;
    if (![nib instantiateWithOwner:owner topLevelObjects:&top]) return 2;
    if (![owner.progressView.identifier isEqualToString:@"_NS:26"] ||
        ![owner.leftTimeLabel.identifier isEqualToString:@"_NS:8"] ||
        ![owner.volumeSlider.identifier isEqualToString:@"_NS:68"]) return 3;
    // Reconstruct viewDidLoad's centered toolbar and first play/pause cell.
    NSRect bar = owner.view.frame, slider = owner.progressView.frame;
    NSMatrix *toolbar = [[NSMatrix alloc] initWithFrame:NSMakeRect((bar.size.width - 300) / 2, 16, 300, 18)
        mode:NSHighlightModeMatrix prototype:[NSButtonCell new] numberOfRows:0 numberOfColumns:0];
    toolbar.cellSize = NSMakeSize(18, 18);
    toolbar.intercellSpacing = NSMakeSize(24, 0);
    for (int i = 0; i < 6; i++) [toolbar addColumn];
    NSRect cell = [toolbar cellFrameAtRow:0 column:0];
    double x = NSMinX(slider) + NSWidth(slider) * atof(argv[2]) + atof(argv[3]);
    double y = NSMidY(slider) - atof(argv[4]); // AX coordinates grow downwards.
    if (fabs(x - (NSMinX(toolbar.frame) + NSMidX(cell))) > 0.01 ||
        fabs(y - (NSMinY(toolbar.frame) + NSMidY(cell))) > 0.01) return 4;
    printf("SzPlayer 26.10.56: progress=_NS:26 clock=_NS:8 volume=_NS:68; playback=(%.1f,%.1f)\n", x, y);
    return 0;
}}
