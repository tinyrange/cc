# CrumbleCracker UI - Starlark Interface
#
# This file demonstrates the React/Tailwind-inspired Starlark UI API for ccapp.
# Place this file in your bundles directory to customize the interface.
#
# Available Components (React-like):
#   Row(children, gap, padding, background, main_align, cross_align)
#   Column(children, gap, padding, background, main_align, cross_align)
#   Stack(children)
#   Center(child)
#   Padding(child, padding)
#   Spacer(width, height)
#   Align(child, alignment)
#   Text(text, size, color)
#   Box(color, width, height)
#   Button(text, on_click, min_width, min_height, background, hover_background, ...)
#   Card(child, on_click, width, height, background, hover_background, border_color, ...)
#   Logo(width, height, speed1, speed2, speed3)
#   ScrollView(child, horizontal, scrollbar_width)
#
# Available Colors (Tailwind-inspired):
#   gray(shade), red(shade), orange(shade), yellow(shade), green(shade),
#   blue(shade), indigo(shade), purple(shade), pink(shade)
#   white(), black(), transparent()
#   rgb(r, g, b), rgba(r, g, b, a)
#
# Shades: 50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950
#
# Spacing:
#   insets(all) - uniform padding
#   insets(h, v) - horizontal, vertical
#   insets(l, t, r, b) - left, top, right, bottom
#
# Alignment:
#   top_left, top_center, top_right
#   center_left, center_center, center_right
#   bottom_left, bottom_center, bottom_right
#
# Main Axis Alignment:
#   main_start, main_center, main_end
#   main_space_between, main_space_around, main_space_evenly
#
# Cross Axis Alignment:
#   cross_start, cross_center, cross_end, cross_stretch

# Color constants matching the original design
COLOR_BACKGROUND = rgba(10, 10, 10, 255)
COLOR_TOP_BAR = rgba(22, 22, 22, 255)
COLOR_BTN_NORMAL = rgba(40, 40, 40, 255)
COLOR_BTN_HOVER = rgba(56, 56, 56, 255)
COLOR_BTN_PRESSED = rgba(72, 72, 72, 255)
COLOR_BORDER_NORMAL = rgba(80, 80, 80, 255)
COLOR_BORDER_HOVER = rgba(140, 140, 140, 255)
COLOR_CARD_BG = rgba(20, 20, 20, 220)
COLOR_CARD_BG_HOVER = rgba(30, 30, 30, 235)
COLOR_OVERLAY = rgba(10, 10, 10, 200)
COLOR_LIGHT_GRAY = gray(400)

def top_bar(ctx, show_exit=False):
    """Creates the top navigation bar."""
    children = []

    if show_exit:
        children.append(
            Button(
                text="Exit",
                min_width=70,
                min_height=20,
                on_click=ctx["stop_vm"],
            )
        )

    children.append(Spacer())
    children.append(
        Button(
            text="Debug Logs",
            min_width=120,
            min_height=20,
            on_click=ctx["open_logs"],
        )
    )

    return Row(
        children=children,
        background=COLOR_TOP_BAR,
        padding=insets(20, 6),
    )

def bundle_card(ctx, bundle):
    """Creates a clickable card for a bundle."""
    card_width = 180
    card_height = 230
    content_width = card_width - 10

    content = Column(
        children=[
            # Image placeholder
            Spacer(width=content_width, height=content_width),
            # Name
            Text(text=bundle["name"], size=18),
            # Description
            Text(text=bundle["description"], size=14, color=COLOR_LIGHT_GRAY),
        ],
        gap=8,
    )

    def on_click():
        ctx["select_bundle"](index=bundle["index"])

    return Card(
        child=content,
        width=card_width,
        height=card_height,
        background=transparent(),
        hover_background=COLOR_CARD_BG,
        border_color=COLOR_BORDER_NORMAL,
        hover_border_color=COLOR_BORDER_HOVER,
        border_width=1,
        padding=insets(5, 0, 5, 0),
        on_click=on_click,
    )

def bundle_cards(ctx):
    """Creates the horizontal scrollable list of bundle cards."""
    bundles = ctx["bundles"]
    if len(bundles) == 0:
        return None

    cards = [bundle_card(ctx, b) for b in bundles]

    card_row = Row(children=cards, gap=24)
    scroll_view = ScrollView(child=card_row, horizontal=True, scrollbar_width=8)

    return Stack(children=[
        Box(color=COLOR_OVERLAY, height=320),
        Column(
            children=[scroll_view],
            padding=insets(20, 0, 20, 20),
        ),
    ])

def title_section(ctx):
    """Creates the title section with app name and subtitle."""
    bundles = ctx["bundles"]
    bundles_dir = ctx["bundles_dir"]

    children = [
        Text(text="CrumbleCracker", size=48),
    ]

    if len(bundles) == 0:
        children.append(
            Padding(
                child=Text(
                    text="No bundles found. Create bundles with: cc -build <outDir> <image>",
                    size=20,
                ),
                padding=insets(0, 10, 0, 0),
            )
        )
        children.append(
            Padding(
                child=Text(text="Searched for bundles in: " + bundles_dir, size=20),
                padding=insets(0, 10, 0, 0),
            )
        )
    else:
        children.append(
            Padding(
                child=Text(text="Please select an environment to boot", size=20),
                padding=insets(0, 10, 0, 10),
            )
        )

    return Column(
        children=children,
        padding=insets(20, 50, 20, 0),
    )

def launcher_screen(ctx):
    """
    The main launcher screen showing available bundles.

    Context provides:
      - ctx["bundles"]: list of bundle dicts with index, name, description, dir
      - ctx["bundles_dir"]: path to bundles directory
      - ctx["open_logs"]: callback to open logs
      - ctx["select_bundle"](index): callback to select and boot a bundle
    """
    bundles = ctx["bundles"]

    # Build main content column
    content_children = [
        top_bar(ctx),
        title_section(ctx),
    ]

    # Add bundle cards if we have any
    bundle_section = bundle_cards(ctx)
    if bundle_section:
        content_children.append(bundle_section)

    content = Column(children=content_children)

    # Build stack with background, logo, and content
    stack_children = [
        Box(color=COLOR_BACKGROUND),
    ]

    # Add logo in bottom-right (partly off-screen)
    if len(bundles) > 0:
        stack_children.append(
            Align(
                child=Padding(
                    child=Logo(width=400, height=400),
                    padding=insets(0, 0, -140, -140),
                ),
                alignment=bottom_right,
            )
        )

    stack_children.append(content)

    return Stack(children=stack_children)

def loading_screen(ctx):
    """
    The loading screen shown while booting a VM.

    Context provides:
      - ctx["boot_name"]: name of the bundle being booted
    """
    boot_name = ctx["boot_name"]
    msg = "Booting VM…"
    if boot_name:
        msg = "Booting " + boot_name + "…"

    return Stack(children=[
        Box(color=COLOR_BACKGROUND),
        Center(
            child=Logo(width=300, height=300, speed1=0.9, speed2=-1.4, speed3=2.2),
        ),
        Align(
            child=Padding(
                child=Text(text=msg, size=20),
                padding=insets(20),
            ),
            alignment=top_left,
        ),
    ])

def error_screen(ctx):
    """
    The error screen shown when something goes wrong.

    Context provides:
      - ctx["error_message"]: the error message to display
      - ctx["back"]: callback to go back to launcher
      - ctx["open_logs"]: callback to open logs
    """
    error_msg = ctx["error_message"]
    if not error_msg:
        error_msg = "unknown error"

    content = Column(
        children=[
            Text(text="Error", size=56),
            Padding(
                child=Text(text=error_msg, size=18),
                padding=insets(0, 30, 0, 0),
            ),
            Padding(
                child=Column(
                    children=[
                        Button(
                            text="Back to carousel",
                            min_width=320,
                            min_height=44,
                            on_click=ctx["back"],
                        ),
                        Button(
                            text="Open logs directory",
                            min_width=320,
                            min_height=44,
                            on_click=ctx["open_logs"],
                        ),
                    ],
                    gap=14,
                ),
                padding=insets(0, 40, 0, 0),
            ),
        ],
        padding=insets(30),
    )

    return Stack(children=[
        Box(color=COLOR_BACKGROUND),
        Center(
            child=Logo(width=250, height=250, speed1=0.4, speed2=0, speed3=0),
        ),
        content,
    ])

# Note: Terminal screen is not customizable via Starlark as it requires
# direct access to the terminal view widget and frame rendering.
