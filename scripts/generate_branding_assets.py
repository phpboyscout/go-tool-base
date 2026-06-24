import os
import math
from PIL import Image, ImageDraw, ImageFilter

def generate_png_variants(output_dir):
    os.makedirs(output_dir, exist_ok=True)
    sizes = [32, 64, 128, 256, 512, 1024]
    bg_types = {
        "dark": (10, 14, 23, 255),
        "light": (248, 250, 252, 255),
        "transparent": (0, 0, 0, 0)
    }
    
    for bg_name, bg_color in bg_types.items():
        for size in sizes:
            scale = 4
            width = size * scale
            height = size * scale
            cx, cy = width / 2, height / 2
            
            R_outer = 190 * (size / 512) * scale
            R_frame_inner = 155 * (size / 512) * scale
            R_inner_hex = 105 * (size / 512) * scale
            R_facet_outer = 186 * (size / 512) * scale
            R_facet_inner = 159 * (size / 512) * scale
            
            def get_hex_points(radius):
                pts = []
                for i in range(6):
                    angle = math.radians(i * 60)
                    x = cx + radius * math.sin(angle)
                    y = cy - radius * math.cos(angle)
                    pts.append((x, y))
                return pts

            outer_pts = get_hex_points(R_outer)
            frame_inner_pts = get_hex_points(R_frame_inner)
            inner_hex_pts = get_hex_points(R_inner_hex)
            
            w_outer = 14 * (size / 512) * scale
            w_frame_inner = 14 * (size / 512) * scale
            w_facet = 10 * (size / 512) * scale
            w_inner_hex = 12 * (size / 512) * scale
            
            uw = 42 * (size / 512) * scale
            uh = 12 * (size / 512) * scale
            gap = 24 * (size / 512) * scale
            uy = cy + (25 * (size / 512) * scale)
            rx = int(2 * (size / 512) * scale)
            
            x1_l = cx - (gap/2) - uw
            x2_l = cx - (gap/2)
            x1_r = cx + (gap/2)
            x2_r = cx + (gap/2) + uw
            y1 = uy
            y2 = uy + uh

            # Use L mode for masking
            mask = Image.new("L", (width, height), 0)
            draw = ImageDraw.Draw(mask)
            
            def draw_thick_hex(r_center, stroke_width):
                r_out = r_center + (stroke_width / 2.0) / math.cos(math.radians(30))
                r_in = r_center - (stroke_width / 2.0) / math.cos(math.radians(30))
                for i in range(6):
                    a1 = math.radians(i * 60)
                    a2 = math.radians((i + 1) * 60)
                    p1 = (cx + r_out * math.sin(a1), cy - r_out * math.cos(a1))
                    p2 = (cx + r_out * math.sin(a2), cy - r_out * math.cos(a2))
                    p3 = (cx + r_in * math.sin(a2), cy - r_in * math.cos(a2))
                    p4 = (cx + r_in * math.sin(a1), cy - r_in * math.cos(a1))
                    draw.polygon([p1, p2, p3, p4], fill=255)

            # Draw facets as mathematically perfect polygons from R_frame_inner to R_outer
            for i in range(6):
                angle = math.radians(i * 60)
                vx, vy = math.sin(angle), -math.cos(angle)
                px, py = math.cos(angle), math.sin(angle)
                r1, r2 = R_frame_inner, R_outer
                hw = w_facet / 2.0
                p1 = (cx + r1*vx + hw*px, cy + r1*vy + hw*py)
                p2 = (cx + r2*vx + hw*px, cy + r2*vy + hw*py)
                p3 = (cx + r2*vx - hw*px, cy + r2*vy - hw*py)
                p4 = (cx + r1*vx - hw*px, cy + r1*vy - hw*py)
                draw.polygon([p1, p2, p3, p4], fill=255)
            
            draw_thick_hex(R_outer, w_outer)
            draw_thick_hex(R_frame_inner, w_frame_inner)
            draw_thick_hex(R_inner_hex, w_inner_hex)
            
            draw.rounded_rectangle([x1_l, y1, x2_l, y2], radius=rx, fill=255)
            draw.rounded_rectangle([x1_r, y1, x2_r, y2], radius=rx, fill=255)
            
            # Glow layers
            sigma1 = 6 * (size / 512) * scale
            sigma2 = 20 * (size / 512) * scale
            
            glow1 = mask.filter(ImageFilter.GaussianBlur(sigma1)) if sigma1 >= 0.1 else mask.copy()
            glow2 = mask.filter(ImageFilter.GaussianBlur(sigma2)) if sigma2 >= 0.1 else mask.copy()
            
            mask_glow2 = glow2.point(lambda p: int(p * 0.6))
            mask_glow1 = glow1.point(lambda p: int(p * 0.8))
            mask_sharp = mask.point(lambda p: int(p * 1.0))
            
            # Gradient overlay
            gradient = Image.new("RGBA", (width, height))
            g_draw = ImageDraw.Draw(gradient)
            max_dist = width + height
            for y_g in range(0, height, 4):
                for x_g in range(0, width, 4):
                    factor = (x_g + y_g) / max_dist
                    r = int((1 - factor) * 0 + factor * 208)
                    g = int((1 - factor) * 240 + factor * 0)
                    b = int((1 - factor) * 255 + factor * 255)
                    g_draw.rectangle([x_g, y_g, x_g+4, y_g+4], fill=(r, g, b, 255))
                
            final_canvas = Image.new("RGBA", (width, height), bg_color)
            
            final_canvas.paste(gradient, (0, 0), mask=mask_glow2)
            final_canvas.paste(gradient, (0, 0), mask=mask_glow1)
            final_canvas.paste(gradient, (0, 0), mask=mask_sharp)
            
            rescaled_img = final_canvas.resize((size, size), Image.Resampling.LANCZOS)
            output_path = os.path.join(output_dir, f"logo_{bg_name}_{size}x{size}.png")
            rescaled_img.save(output_path, "PNG")
            
            if size == 512:
                std_path = os.path.join(output_dir, f"logo_{bg_name}.png")
                rescaled_img.save(std_path, "PNG")
                
            print(f"Generated {output_path}")

def generate_svgs(output_dir):
    os.makedirs(output_dir, exist_ok=True)
    
    defs_block = """  <defs>
    <linearGradient id="cyan-violet-gradient" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="512" y2="512">
      <stop offset="0%" stop-color="#00f0ff" />
      <stop offset="100%" stop-color="#d000ff" />
    </linearGradient>

    <filter id="neon-glow" filterUnits="userSpaceOnUse" x="-200" y="-200" width="1000" height="1000">
      <feGaussianBlur stdDeviation="6" result="blur1" />
      <feGaussianBlur stdDeviation="20" result="blur2" />
      <feMerge>
        <feMergeNode in="blur2" opacity="0.6"/>
        <feMergeNode in="blur1" opacity="0.8"/>
        <feMergeNode in="SourceGraphic" />
      </feMerge>
    </filter>

    <style>
      .bg-dark { fill: #0a0e17; }
      .bg-light { fill: #f8fafc; }
      .glowing-shape {
        fill: none;
        stroke: url(#cyan-violet-gradient);
        stroke-linejoin: round;
        
        filter: url(#neon-glow);
      }
      .w-outer { stroke-width: 14; }
      .w-frame-inner { stroke-width: 14; }
      .w-facet { stroke-width: 10; }
      .w-inner { stroke-width: 12; }
      .underscores {
        fill: url(#cyan-violet-gradient);
        filter: url(#neon-glow);
      }
    </style>
  </defs>"""

    shapes_block = """  <!-- 6 Facet Lines -->
  <line x1="256" y1="66" x2="256" y2="101" class="glowing-shape w-facet" />
  <line x1="420.5" y1="161" x2="390.2" y2="178.5" class="glowing-shape w-facet" />
  <line x1="420.5" y1="351" x2="390.2" y2="333.5" class="glowing-shape w-facet" />
  <line x1="256" y1="446" x2="256" y2="411" class="glowing-shape w-facet" />
  <line x1="91.5" y1="351" x2="121.8" y2="333.5" class="glowing-shape w-facet" />
  <line x1="91.5" y1="161" x2="121.8" y2="178.5" class="glowing-shape w-facet" />

  <!-- Outer Hexagon -->
  <polygon points="256,66 420.5,161 420.5,351 256,446 91.5,351 91.5,161" class="glowing-shape w-outer" />
  
  <!-- Frame Inner Hexagon -->
  <polygon points="256,101 390.2,178.5 390.2,333.5 256,411 121.8,333.5 121.8,178.5" class="glowing-shape w-frame-inner" />

  <!-- Inner Hexagon -->
  <polygon points="256,151 346.9,203.5 346.9,308.5 256,361 165.1,308.5 165.1,203.5" class="glowing-shape w-inner" />

  <!-- Underscores -->
  <rect x="202" y="281" width="42" height="12" rx="2" class="underscores" />
  <rect x="268" y="281" width="42" height="12" rx="2" class="underscores" />"""

    logo_dark_content = f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="100%" height="100%">
{defs_block}
  <!-- Solid Dark Background -->
  <rect width="512" height="512" class="bg-dark" />
{shapes_block}
</svg>"""

    logo_light_content = f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="100%" height="100%">
{defs_block}
  <!-- Solid Light Background -->
  <rect width="512" height="512" class="bg-light" />
{shapes_block}
</svg>"""

    logo_trans_content = f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="100%" height="100%">
{defs_block}
{shapes_block}
</svg>"""

    with open(os.path.join(output_dir, "logo.svg"), "w") as f:
        f.write(logo_dark_content)
    with open(os.path.join(output_dir, "logo_light.svg"), "w") as f:
        f.write(logo_light_content)
    with open(os.path.join(output_dir, "logo_transparent.svg"), "w") as f:
        f.write(logo_trans_content)
        
    print("Generated SVGs successfully.")

if __name__ == "__main__":
    target_dir = "/home/matt/workspace/phpboyscout/go-tool-base-style/docs/images/branding"
    generate_png_variants(target_dir)
    generate_svgs(target_dir)
