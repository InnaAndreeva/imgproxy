package processing

import (
	"context"
	"math"

	"github.com/imgproxy/imgproxy/v3/config"
	"github.com/imgproxy/imgproxy/v3/imagedata"
	"github.com/imgproxy/imgproxy/v3/imath"
	"github.com/imgproxy/imgproxy/v3/options"
	"github.com/imgproxy/imgproxy/v3/vips"
)

var watermarkPipeline = pipeline{
	prepare,
	scaleOnLoad,
	importColorProfile,
	scale,
	rotateAndFlip,
	padding,
}

func prepareWatermark(wm *vips.Image, wmData *imagedata.ImageData, opts *options.WatermarkOptions, imgWidth, imgHeight int, offsetScale float64, framesCount int) error {
	if err := wm.Load(wmData, 1, 1.0, 1); err != nil {
		return err
	}

	po := options.NewProcessingOptions()
	po.ResizingType = options.ResizeFit
	po.Dpr = 1
	po.Enlarge = true
	po.Format = wmData.Type

	if opts.Scale > 0 {
		po.Width = imath.Max(imath.ScaleToEven(imgWidth, opts.Scale), 1)
		po.Height = imath.Max(imath.ScaleToEven(imgHeight, opts.Scale), 1)
	}

	if opts.Replicate {
		var offX, offY int

		if math.Abs(opts.Gravity.X) >= 1.0 {
			offX = imath.RoundToEven(opts.Gravity.X * offsetScale)
		} else {
			offX = imath.ScaleToEven(imgWidth, opts.Gravity.X)
		}

		if math.Abs(opts.Gravity.Y) >= 1.0 {
			offY = imath.RoundToEven(opts.Gravity.Y * offsetScale)
		} else {
			offY = imath.ScaleToEven(imgHeight, opts.Gravity.Y)
		}

		po.Padding.Enabled = true
		po.Padding.Left = offX / 2
		po.Padding.Right = offX - po.Padding.Left
		po.Padding.Top = offY / 2
		po.Padding.Bottom = offY - po.Padding.Top
	}

	if err := watermarkPipeline.Run(context.Background(), wm, po, wmData); err != nil {
		return err
	}

	if opts.Replicate || framesCount > 1 {
		// We need to copy image if we're going to replicate.
		// Replication requires image to be read several times, and this requires
		// random access to pixels
		if err := wm.CopyMemory(); err != nil {
			return err
		}
	}

	if opts.Replicate {
		if err := wm.Replicate(imgWidth, imgHeight); err != nil {
			return err
		}
	}

	// We don't want any headers to be copied from the watermark to the image
	if err := wm.StripAll(); err != nil {
		return err
	}

	return nil
}

func applyWatermark(img *vips.Image, wmData *imagedata.ImageData, opts *options.WatermarkOptions, offsetScale float64, framesCount int) error {
	wm := new(vips.Image)
	defer wm.Clear()

	width := img.Width()
	height := img.Height()
	frameHeight := height / framesCount

	if err := prepareWatermark(wm, wmData, opts, width, frameHeight, offsetScale, framesCount); err != nil {
		return err
	}

	if !img.ColourProfileImported() {
		if err := img.ImportColourProfile(); err != nil {
			return err
		}
	}

	if err := img.RgbColourspace(); err != nil {
		return err
	}

	opacity := opts.Opacity * config.WatermarkOpacity

	// If we replicated the watermark and need to apply it to an animated image,
	// it is faster to replicate the watermark to all the image and apply it single-pass
	if opts.Replicate && framesCount > 1 {
		if err := wm.Replicate(width, height); err != nil {
			return err
		}

		return img.ApplyWatermark(wm, 0, 0, opacity)
	}

	left, top := 0, 0

	if !opts.Replicate {
		left, top = calcPosition(width, frameHeight, wm.Width(), wm.Height(), &opts.Gravity, offsetScale, true)
	}

	for i := 0; i < framesCount; i++ {
		if err := img.ApplyWatermark(wm, left, top, opacity); err != nil {
			return err
		}
		top += frameHeight
	}

	return nil
}

func watermark(pctx *pipelineContext, img *vips.Image, po *options.ProcessingOptions, imgdata *imagedata.ImageData) error {
	if !po.Watermark.Enabled || imagedata.Watermark == nil {
		return nil
	}

	return applyWatermark(img, imagedata.Watermark, &po.Watermark, pctx.dprScale, 1)
}
