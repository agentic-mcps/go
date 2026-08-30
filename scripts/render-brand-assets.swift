#!/usr/bin/env swift

import AppKit
import Foundation

let brandDirectory = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first ?? "assets/brand", isDirectory: true)
let markURL = brandDirectory.appendingPathComponent("project-mark.svg")

guard let mark = NSImage(contentsOf: markURL) else {
    fatalError("cannot load \(markURL.path)")
}

func color(_ red: Int, _ green: Int, _ blue: Int, alpha: CGFloat = 1) -> NSColor {
    NSColor(
        calibratedRed: CGFloat(red) / 255,
        green: CGFloat(green) / 255,
        blue: CGFloat(blue) / 255,
        alpha: alpha
    )
}

func roundedRect(_ rect: NSRect, radius: CGFloat, fill: NSColor, stroke: NSColor? = nil) {
    let path = NSBezierPath(roundedRect: rect, xRadius: radius, yRadius: radius)
    fill.setFill()
    path.fill()
    if let stroke {
        stroke.setStroke()
        path.lineWidth = 2
        path.stroke()
    }
}

func text(_ value: String, at point: NSPoint, size: CGFloat, weight: NSFont.Weight, color: NSColor, kern: CGFloat = 0) {
    let attributes: [NSAttributedString.Key: Any] = [
        .font: NSFont.systemFont(ofSize: size, weight: weight),
        .foregroundColor: color,
        .kern: kern,
    ]
    value.draw(at: point, withAttributes: attributes)
}

func render(width: Int, height: Int, draw: () -> Void) -> Data {
    guard let bitmap = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: width,
        pixelsHigh: height,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ) else {
        fatalError("cannot allocate \(width)x\(height) bitmap")
    }
    guard let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
        fatalError("cannot create graphics context")
    }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = context
    draw()
    context.flushGraphics()
    NSGraphicsContext.restoreGraphicsState()
    guard let png = bitmap.representation(using: .png, properties: [.compressionFactor: 1]) else {
        fatalError("cannot encode PNG")
    }
    return png
}

let avatar = render(width: 1024, height: 1024) {
    color(245, 245, 242).setFill()
    NSRect(x: 0, y: 0, width: 1024, height: 1024).fill()
    roundedRect(NSRect(x: 64, y: 64, width: 896, height: 896), radius: 220, fill: .white, stroke: color(222, 222, 216))
    mark.draw(in: NSRect(x: 152, y: 130, width: 720, height: 720), from: .zero, operation: .sourceOver, fraction: 1)
}
try avatar.write(to: brandDirectory.appendingPathComponent("organization-avatar.png"), options: .atomic)

let socialPreview = render(width: 1280, height: 640) {
    color(245, 245, 242).setFill()
    NSRect(x: 0, y: 0, width: 1280, height: 640).fill()
    roundedRect(NSRect(x: 56, y: 56, width: 1168, height: 528), radius: 52, fill: .white, stroke: color(222, 222, 216))

    roundedRect(NSRect(x: 104, y: 486, width: 196, height: 42), radius: 21, fill: color(13, 17, 23))
    color(0, 173, 216).setFill()
    NSBezierPath(ovalIn: NSRect(x: 123, y: 500, width: 14, height: 14)).fill()
    text("LOCAL GO MCP", at: NSPoint(x: 150, y: 497), size: 18, weight: .semibold, color: .white, kern: 1.2)

    text("agentic-go", at: NSPoint(x: 104, y: 343), size: 78, weight: .bold, color: color(13, 17, 23), kern: -2.5)
    text("Go intelligence that stays with the change.", at: NSPoint(x: 108, y: 278), size: 31, weight: .medium, color: color(63, 72, 82))
    text("Navigate. Keep context. Refactor safely. Verify with evidence.", at: NSPoint(x: 108, y: 218), size: 23, weight: .regular, color: color(104, 113, 123))

    mark.draw(in: NSRect(x: 856, y: 160, width: 300, height: 300), from: .zero, operation: .sourceOver, fraction: 1)
    roundedRect(NSRect(x: 846, y: 106, width: 320, height: 48), radius: 24, fill: color(238, 249, 252), stroke: color(185, 232, 243))
    text("source-grounded · stdio-only", at: NSPoint(x: 878, y: 121), size: 18, weight: .semibold, color: color(0, 117, 141))
}
try socialPreview.write(to: brandDirectory.appendingPathComponent("social-preview.png"), options: .atomic)
