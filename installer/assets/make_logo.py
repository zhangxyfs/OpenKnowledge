#!/usr/bin/env python3
"""生成 OpenKnowledge logo（logo.png 256x256 + logo.ico 多尺寸）。

设计：蓝色圆角方块底 + 白色翻开的书（知识）+ 右上琥珀色星点（AI）。
输出到 installer/assets/。运行：python installer/assets/make_logo.py
"""
from pathlib import Path

from PIL import Image, ImageDraw

SIZE = 256
BG = (37, 99, 235, 255)        # #2563eb
WHITE = (255, 255, 255, 255)
AMBER = (251, 191, 36, 255)    # #fbbf24

OUT = Path(__file__).resolve().parent


def make(size: int) -> Image.Image:
    s = size / SIZE
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)

    # 圆角方块底
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=int(56 * s), fill=BG)

    # 白色翻开的书：左右两页（顶边外低内高、底边外高内低，中间留书脊缝）
    # 左页：外上(56,80) 内上(120,94) 内下(120,182) 外下(56,164)
    d.polygon([(56 * s, 80 * s), (120 * s, 94 * s), (120 * s, 182 * s), (56 * s, 164 * s)], fill=WHITE)
    # 右页：内上(136,94) 外上(200,80) 外下(200,164) 内下(136,182)
    d.polygon([(136 * s, 94 * s), (200 * s, 80 * s), (200 * s, 164 * s), (136 * s, 182 * s)], fill=WHITE)

    # 右上星点（AI）：琥珀色四芒星
    sx, sy, r = (190 * s, 56 * s, 24 * s)
    d.polygon([(sx, sy - r), (sx + r * 0.32, sy - r * 0.32), (sx + r, sy),
               (sx + r * 0.32, sy + r * 0.32), (sx, sy + r), (sx - r * 0.32, sy + r * 0.32),
               (sx - r, sy), (sx - r * 0.32, sy - r * 0.32)], fill=AMBER)
    return img


def main():
    img = make(SIZE)
    png = OUT / "logo.png"
    img.save(png)
    print(f"written: {png}")

    sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    icons = [make(w) for w, _ in sizes]
    ico = OUT / "logo.ico"
    icons[0].save(ico, format="ICO", sizes=sizes, append_images=icons[1:])
    print(f"written: {ico}")


if __name__ == "__main__":
    main()
