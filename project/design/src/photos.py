# python3 photos.py — кадрирование и мягкая цветокоррекция фото под палитру (без «ИИ-тонирования»)
import os
from PIL import Image, ImageEnhance
H = os.path.dirname(os.path.abspath(__file__)); A = os.path.join(H, '..', 'assets')
# файл: (ширина, высота в px для 2x, фокус x, фокус y) — фокус на лицах, лица не режем
JOBS = {
  'cafe':    (564, 800, .36, .45),
  'barber':  (564, 800, .50, .42),
  'florist': (564, 800, .52, .40),
  'fitness': (564, 800, .55, .45),
  'creator': (1040, 1160, .52, .50),
}
def crop(im, w, h, fx, fy):
    W, Hh = im.size; r = w / h
    if W / Hh > r: cw, ch = int(Hh * r), Hh
    else: cw, ch = W, int(W / r)
    x = min(max(int(fx * W - cw / 2), 0), W - cw); y = min(max(int(fy * Hh - ch / 2), 0), Hh - ch)
    return im.crop((x, y, x + cw, y + ch)).resize((w, h), Image.LANCZOS)
def soften(im):
    im = ImageEnhance.Color(im).enhance(.86)        # чуть меньше насыщенности
    im = ImageEnhance.Contrast(im).enhance(.92)     # мягче контраст
    warm = Image.new('RGB', im.size, (243, 238, 226)) # тёплая подложка, как светлый фон страницы
    return Image.blend(im, warm, .06)
for n, (w, h, fx, fy) in JOBS.items():
    im = Image.open(os.path.join(A, 'orig', n + '.jpg')).convert('RGB')
    soften(crop(im, w, h, fx, fy)).save(os.path.join(A, n + '.jpg'), quality=88)
    print(n, w, h)
