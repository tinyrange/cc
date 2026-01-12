// Command html is a kitchen sink demo for the HTML renderer.
// It renders various HTML elements with utility CSS classes and takes a screenshot.
//
// Usage:
//
//	go run ./internal/cmd/html -o screenshot.png
package main

import (
	"flag"
	"fmt"
	"image/color"
	"image/png"
	"os"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/gowin/ui/html"
)

// CrumbleCracker-style demo using the fruity color palette
const crumbleCrackerHTML = `
<div class="flex flex-col h-screen bg-canvas">
	<!-- Top Bar -->
	<div class="w-full h-14 bg-bar border-b border-border-light flex flex-row items-center justify-between px-5">
		<!-- Logo & Title -->
		<div class="flex flex-row items-center gap-3">
			<div class="w-9 h-9 rounded-xl bg-gradient-to-br from-mango-400 via-berry-500 to-grape-500 flex items-center justify-center">
				<svg viewBox="0 0 24 24" fill="white" class="w-5 h-5">
					<polygon points="12,2 2,19 22,19"/>
				</svg>
			</div>
			<h1 class="text-2xl gradient-text">CrumbleCracker</h1>
		</div>
		<!-- Right side -->
		<div class="flex flex-row items-center gap-3">
			<span class="text-xs text-ink-400 bg-canvas px-2 py-1 rounded">vdev</span>
			<div class="w-8 h-8 rounded-full bg-gradient-to-br from-lime-400 to-ocean-500 flex items-center justify-center">
				<span class="text-xs text-white">JD</span>
			</div>
		</div>
	</div>

	<!-- Tab Bar -->
	<div class="w-full h-12 bg-tab-inactive border-b border-border-light flex flex-row items-center px-5">
		<div class="flex flex-row gap-1">
			<button class="px-5 py-2 rounded-t-lg text-sm bg-tab-active text-ink-800">Bundles</button>
			<button class="px-5 py-2 rounded-t-lg text-sm bg-tab-inactive text-ink-500">Settings</button>
		</div>
		<div class="flex flex-row flex-1 justify-end">
			<button class="flex flex-row items-center gap-2 px-4 py-2 bg-gradient-to-r from-mango-500 to-berry-500 text-white rounded-lg text-sm">
				+ New Bundle
			</button>
		</div>
	</div>

	<!-- Main Content -->
	<div class="flex-1 p-6 bg-canvas">
		<!-- Bundle Row 1 - Alpine Running -->
		<div class="bg-surface rounded-xl border border-border-light p-4 mb-3">
			<div class="flex flex-row items-center gap-4">
				<div class="w-12 h-12 rounded-xl bg-gradient-to-br from-mango-400 to-mango-600 flex items-center justify-center">
					<svg viewBox="0 0 24 24" fill="white" class="w-6 h-6">
						<polygon points="12,2 2,19 22,19"/>
					</svg>
				</div>
				<div class="flex-1">
					<span class="text-base text-ink-900">Alpine Linux 3.20</span>
					<div class="flex flex-row items-center gap-1 mt-1">
						<div class="w-2 h-2 rounded-full bg-lime-500"></div>
						<span class="text-xs text-lime-600">Running</span>
					</div>
				</div>
				<div class="w-24">
					<span class="text-xs text-ink-500">2GB RAM</span>
					<div class="h-1 bg-border-light rounded-full mt-1">
						<div class="h-1 bg-lime-500 rounded-full" style="width: 65%"></div>
					</div>
				</div>
				<div class="w-20">
					<span class="text-xs text-ink-500">2 vCPU</span>
					<div class="h-1 bg-border-light rounded-full mt-1">
						<div class="h-1 bg-lime-500 rounded-full" style="width: 40%"></div>
					</div>
				</div>
				<div class="flex flex-row gap-2">
					<button class="w-9 h-9 flex items-center justify-center bg-berry-500 text-white rounded-lg">
						<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
							<rect x="6" y="6" width="12" height="12"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<rect x="4" y="4" width="16" height="16" rx="2"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<circle cx="12" cy="12" r="2"/>
							<circle cx="12" cy="5" r="2"/>
							<circle cx="12" cy="19" r="2"/>
						</svg>
					</button>
				</div>
			</div>
		</div>

		<!-- Bundle Row 2 - Kali Stopped -->
		<div class="bg-surface rounded-xl border border-border-light p-4 mb-3">
			<div class="flex flex-row items-center gap-4">
				<div class="w-12 h-12 rounded-xl bg-gradient-to-br from-grape-400 to-grape-600 flex items-center justify-center">
					<svg viewBox="0 0 24 24" fill="white" class="w-6 h-6">
						<polygon points="8,5 19,12 8,19"/>
					</svg>
				</div>
				<div class="flex-1">
					<span class="text-base text-ink-900">Kali Rolling</span>
					<div class="flex flex-row items-center gap-1 mt-1">
						<div class="w-2 h-2 rounded-full bg-ink-300"></div>
						<span class="text-xs text-ink-400">Stopped</span>
					</div>
				</div>
				<div class="w-24">
					<span class="text-xs text-ink-500">4GB RAM</span>
					<div class="h-1 bg-border-light rounded-full mt-1"></div>
				</div>
				<div class="w-20">
					<span class="text-xs text-ink-500">4 vCPU</span>
					<div class="h-1 bg-border-light rounded-full mt-1"></div>
				</div>
				<div class="flex flex-row gap-2">
					<button class="w-9 h-9 flex items-center justify-center bg-lime-500 text-white rounded-lg">
						<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
							<polygon points="8,5 19,12 8,19"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<rect x="4" y="4" width="16" height="16" rx="2"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<circle cx="12" cy="12" r="2"/>
							<circle cx="12" cy="5" r="2"/>
							<circle cx="12" cy="19" r="2"/>
						</svg>
					</button>
				</div>
			</div>
		</div>

		<!-- Bundle Row 3 - Nilmath Stopped -->
		<div class="bg-surface rounded-xl border border-border-light p-4 mb-3">
			<div class="flex flex-row items-center gap-4">
				<div class="w-12 h-12 rounded-xl bg-gradient-to-br from-mango-400 to-citrus-500 flex items-center justify-center">
					<svg viewBox="0 0 24 24" fill="white" class="w-6 h-6">
						<polygon points="8,5 19,12 8,19"/>
					</svg>
				</div>
				<div class="flex-1">
					<span class="text-base text-ink-900">Nilmath 1.0</span>
					<div class="flex flex-row items-center gap-1 mt-1">
						<div class="w-2 h-2 rounded-full bg-ink-300"></div>
						<span class="text-xs text-ink-400">Stopped</span>
					</div>
				</div>
				<div class="w-24">
					<span class="text-xs text-ink-500">8GB RAM</span>
					<div class="h-1 bg-border-light rounded-full mt-1"></div>
				</div>
				<div class="w-20">
					<span class="text-xs text-ink-500">4 vCPU</span>
					<div class="h-1 bg-border-light rounded-full mt-1"></div>
				</div>
				<div class="flex flex-row gap-2">
					<button class="w-9 h-9 flex items-center justify-center bg-lime-500 text-white rounded-lg">
						<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
							<polygon points="8,5 19,12 8,19"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<rect x="4" y="4" width="16" height="16" rx="2"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<circle cx="12" cy="12" r="2"/>
							<circle cx="12" cy="5" r="2"/>
							<circle cx="12" cy="19" r="2"/>
						</svg>
					</button>
				</div>
			</div>
		</div>

		<!-- Bundle Row 4 - Ubuntu Stopped -->
		<div class="bg-surface rounded-xl border border-border-light p-4 mb-3">
			<div class="flex flex-row items-center gap-4">
				<div class="w-12 h-12 rounded-xl bg-gradient-to-br from-berry-400 to-berry-600 flex items-center justify-center">
					<svg viewBox="0 0 24 24" fill="white" class="w-6 h-6">
						<polygon points="8,5 19,12 8,19"/>
					</svg>
				</div>
				<div class="flex-1">
					<span class="text-base text-ink-900">Ubuntu 24.04</span>
					<div class="flex flex-row items-center gap-1 mt-1">
						<div class="w-2 h-2 rounded-full bg-ink-300"></div>
						<span class="text-xs text-ink-400">Stopped</span>
					</div>
				</div>
				<div class="w-24">
					<span class="text-xs text-ink-500">8GB RAM</span>
					<div class="h-1 bg-border-light rounded-full mt-1"></div>
				</div>
				<div class="w-20">
					<span class="text-xs text-ink-500">6 vCPU</span>
					<div class="h-1 bg-border-light rounded-full mt-1"></div>
				</div>
				<div class="flex flex-row gap-2">
					<button class="w-9 h-9 flex items-center justify-center bg-lime-500 text-white rounded-lg">
						<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
							<polygon points="8,5 19,12 8,19"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<rect x="4" y="4" width="16" height="16" rx="2"/>
						</svg>
					</button>
					<button class="w-9 h-9 flex items-center justify-center bg-ink-100 text-ink-600 rounded-lg">
						<svg viewBox="0 0 24 24" fill="#555" class="w-4 h-4">
							<circle cx="12" cy="12" r="2"/>
							<circle cx="12" cy="5" r="2"/>
							<circle cx="12" cy="19" r="2"/>
						</svg>
					</button>
				</div>
			</div>
		</div>

		<!-- Bottom Section: Resource Monitor and Quick Actions side by side -->
		<div class="flex flex-row gap-6 mt-4">
			<!-- Resource Monitor -->
			<div class="bg-surface rounded-xl border border-border-light p-4 flex-1">
				<div class="flex flex-row justify-between items-center mb-4">
					<span class="text-base text-ink-800">Resource Monitor</span>
					<div class="flex flex-row gap-3">
						<div class="flex flex-row items-center gap-1">
							<div class="w-2 h-2 rounded-full bg-ocean-500"></div>
							<span class="text-xs text-ink-500">RAM</span>
						</div>
						<div class="flex flex-row items-center gap-1">
							<div class="w-2 h-2 rounded-full bg-mango-500"></div>
							<span class="text-xs text-ink-500">CPU</span>
						</div>
					</div>
				</div>
				<!-- Chart placeholder -->
				<div class="h-24 bg-canvas rounded-lg flex items-center justify-center">
					<span class="text-xs text-ink-400">Chart</span>
				</div>
			</div>

			<!-- Quick Actions -->
			<div class="flex-1">
				<span class="text-base text-ink-800 mb-3">Quick Actions</span>
				<div class="flex flex-col gap-3 mt-3">
					<div class="flex flex-row gap-3">
						<div class="bg-surface rounded-xl border border-border-light p-3 flex-1">
							<div class="flex flex-row items-center gap-2">
								<div class="w-8 h-8 rounded-lg bg-gradient-to-br from-mango-400 to-mango-500 flex items-center justify-center">
									<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
										<polygon points="12,2 12,12 22,12"/>
										<polygon points="12,12 2,12 12,22"/>
									</svg>
								</div>
								<div>
									<span class="text-sm text-ink-800">Import</span>
									<div class="text-xs text-ink-400">Load Bundle</div>
								</div>
							</div>
						</div>
						<div class="bg-surface rounded-xl border border-border-light p-3 flex-1">
							<div class="flex flex-row items-center gap-2">
								<div class="w-8 h-8 rounded-lg bg-gradient-to-br from-grape-400 to-grape-500 flex items-center justify-center">
									<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
										<polygon points="12,22 12,12 2,12"/>
										<polygon points="12,12 22,12 12,2"/>
									</svg>
								</div>
								<div>
									<span class="text-sm text-ink-800">Export</span>
									<div class="text-xs text-ink-400">Save Bundle</div>
								</div>
							</div>
						</div>
					</div>
					<div class="flex flex-row gap-3">
						<div class="bg-surface rounded-xl border border-border-light p-3 flex-1">
							<div class="flex flex-row items-center gap-2">
								<div class="w-8 h-8 rounded-lg bg-gradient-to-br from-lime-400 to-lime-500 flex items-center justify-center">
									<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
										<rect x="4" y="4" width="7" height="7"/>
										<rect x="13" y="13" width="7" height="7"/>
									</svg>
								</div>
								<div>
									<span class="text-sm text-ink-800">Clone</span>
									<div class="text-xs text-ink-400">Duplicate</div>
								</div>
							</div>
						</div>
						<div class="bg-surface rounded-xl border border-border-light p-3 flex-1">
							<div class="flex flex-row items-center gap-2">
								<div class="w-8 h-8 rounded-lg bg-gradient-to-br from-berry-400 to-berry-500 flex items-center justify-center">
									<svg viewBox="0 0 24 24" fill="white" class="w-4 h-4">
										<circle cx="12" cy="12" r="8"/>
									</svg>
								</div>
								<div>
									<span class="text-sm text-ink-800">Docs</span>
									<div class="text-xs text-ink-400">Get Help</div>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	</div>

	<!-- Bottom Bar -->
	<div class="w-full h-10 bg-bar border-t border-border-light flex flex-row items-center justify-between px-5">
		<div class="flex flex-row items-center gap-4">
			<span class="text-xs text-ink-400">v1.0.0</span>
			<span class="text-xs text-ink-500">4 Bundles</span>
			<span class="text-xs text-lime-600">1 running</span>
		</div>
		<div class="flex flex-row items-center gap-4">
			<span class="text-xs text-ink-400">Documentation</span>
			<span class="text-xs text-ink-400">Report Issue</span>
		</div>
	</div>
</div>
`

const kitchenSinkHTML = `
<div class="flex flex-col gap-6 p-8 bg-background">
	<!-- Header -->
	<div class="flex flex-row justify-between items-center">
		<h1 class="text-3xl text-primary">HTML Kitchen Sink</h1>
		<span class="text-sm text-muted">utility CSS + gowin/ui</span>
	</div>

	<!-- Cards Row -->
	<div class="flex flex-row gap-4">
		<!-- Card 1: Text Styles -->
		<div class="flex flex-col gap-3 p-6 bg-card rounded-lg">
			<h2 class="text-xl text-primary">Typography</h2>
			<span class="text-xs text-secondary">text-xs (12px)</span>
			<span class="text-sm text-secondary">text-sm (14px)</span>
			<span class="text-base text-secondary">text-base (16px)</span>
			<span class="text-lg text-secondary">text-lg (18px)</span>
			<span class="text-xl text-primary">text-xl (20px)</span>
			<span class="text-2xl text-primary">text-2xl (24px)</span>
		</div>

		<!-- Card 2: Colors -->
		<div class="flex flex-col gap-3 p-6 bg-card rounded-lg">
			<h2 class="text-xl text-primary">Colors</h2>
			<span class="text-base text-primary">text-primary</span>
			<span class="text-base text-secondary">text-secondary</span>
			<span class="text-base text-muted">text-muted</span>
			<span class="text-base text-accent">text-accent</span>
			<span class="text-base text-success">text-success</span>
			<span class="text-base text-danger">text-danger</span>
			<span class="text-base text-warning">text-warning</span>
		</div>

		<!-- Card 3: Buttons -->
		<div class="flex flex-col gap-3 p-6 bg-card rounded-lg">
			<h2 class="text-xl text-primary">Buttons</h2>
			<button class="bg-btn text-primary px-4 py-2 rounded">Default</button>
			<button class="bg-accent text-dark px-4 py-2 rounded">Primary</button>
			<button class="bg-success text-dark px-4 py-2 rounded">Success</button>
			<button class="bg-danger text-dark px-4 py-2 rounded">Danger</button>
			<button class="bg-warning text-dark px-4 py-2 rounded">Warning</button>
		</div>
	</div>

	<!-- Spacing & Layout Demo -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-lg">
		<h2 class="text-xl text-primary">Layout & Spacing</h2>

		<div class="flex flex-row gap-2">
			<div class="p-2 bg-accent rounded-sm">
				<span class="text-sm text-dark">p-2</span>
			</div>
			<div class="p-4 bg-accent rounded-sm">
				<span class="text-sm text-dark">p-4</span>
			</div>
			<div class="p-6 bg-accent rounded-sm">
				<span class="text-sm text-dark">p-6</span>
			</div>
		</div>

		<div class="flex flex-row gap-4 items-center">
			<span class="text-sm text-secondary">Justify:</span>
			<div class="flex flex-row justify-start p-2 bg-btn rounded" style="width: 200px">
				<span class="text-xs text-muted">start</span>
			</div>
			<div class="flex flex-row justify-center p-2 bg-btn rounded" style="width: 200px">
				<span class="text-xs text-muted">center</span>
			</div>
			<div class="flex flex-row justify-end p-2 bg-btn rounded" style="width: 200px">
				<span class="text-xs text-muted">end</span>
			</div>
		</div>
	</div>

	<!-- Border Radius Demo -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-lg">
		<h2 class="text-xl text-primary">Border Radius</h2>
		<div class="flex flex-row gap-4 items-end">
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-none">
					<span class="text-xs text-dark">none</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-sm">
					<span class="text-xs text-dark">sm</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-md">
					<span class="text-xs text-dark">md</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-lg">
					<span class="text-xs text-dark">lg</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-xl">
					<span class="text-xs text-dark">xl</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-2xl">
					<span class="text-xs text-dark">2xl</span>
				</div>
			</div>
		</div>
	</div>

	<!-- Dialog Example -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-xl">
		<h3 class="text-lg text-primary">Example Dialog</h3>
		<p class="text-secondary text-sm">This demonstrates a typical dialog layout with title, description, and action buttons.</p>
		<div class="flex flex-row gap-3 justify-end">
			<button data-onclick="cancel" class="bg-btn text-primary px-5 py-2 rounded-md">Cancel</button>
			<button data-onclick="confirm" class="bg-accent text-dark px-5 py-2 rounded-md">Confirm</button>
		</div>
	</div>

	<!-- Inputs -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-lg">
		<h2 class="text-xl text-primary">Form Inputs</h2>
		<div class="flex flex-row gap-4 items-center">
			<label class="text-sm text-secondary">Text Input:</label>
			<input type="text" placeholder="Enter text here..." class="bg-btn text-primary p-3 rounded-md" />
		</div>
		<div class="flex flex-row gap-4 items-center">
			<label class="text-sm text-secondary">Checkbox:</label>
			<input type="checkbox" data-bind="checkbox1" />
		</div>
	</div>
</div>
`

// Font diagnostic demo to test text rendering issues
const fontDiagnosticHTML = `
<div class="flex flex-col gap-6 p-6 bg-canvas">
	<h1 class="text-2xl text-ink-900">Font Rendering Diagnostic</h1>

	<!-- Test 1: Baseline alignment - different sizes on same row should align -->
	<div class="bg-surface p-4 rounded-lg">
		<span class="text-sm text-ink-500 mb-2">Test 1: Baseline Alignment (all text should align on same baseline)</span>
		<div class="flex flex-row items-end gap-2 bg-white p-3 rounded mt-2">
			<span class="text-xs text-ink-900">xs</span>
			<span class="text-sm text-ink-900">sm</span>
			<span class="text-base text-ink-900">base</span>
			<span class="text-lg text-ink-900">lg</span>
			<span class="text-xl text-ink-900">xl</span>
			<span class="text-2xl text-ink-900">2xl</span>
		</div>
	</div>

	<!-- Test 2: Kerning pairs -->
	<div class="bg-surface p-4 rounded-lg">
		<span class="text-sm text-ink-500 mb-2">Test 2: Kerning Pairs (spacing should look natural)</span>
		<div class="flex flex-col gap-1 mt-2">
			<span class="text-xl text-ink-900">AVATAR WAVE Type LT AV AW To We</span>
			<span class="text-xl text-ink-900">VA WA TA Yo Te Tr</span>
			<span class="text-base text-ink-900">Spacing: "quotes" and 'apostrophes'</span>
		</div>
	</div>

	<!-- Test 3: AA transition (around 8px boundary) -->
	<div class="bg-surface p-4 rounded-lg">
		<span class="text-sm text-ink-500 mb-2">Test 3: Antialiasing Transition (quality should be consistent)</span>
		<div class="flex flex-col gap-0 bg-white p-3 rounded mt-2">
			<span style="font-size: 6px" class="text-ink-900">6px: The quick brown fox jumps</span>
			<span style="font-size: 7px" class="text-ink-900">7px: The quick brown fox jumps</span>
			<span style="font-size: 8px" class="text-ink-900">8px: The quick brown fox jumps</span>
			<span style="font-size: 9px" class="text-ink-900">9px: The quick brown fox jumps</span>
			<span style="font-size: 10px" class="text-ink-900">10px: The quick brown fox jumps</span>
			<span style="font-size: 11px" class="text-ink-900">11px: The quick brown fox jumps</span>
			<span style="font-size: 12px" class="text-ink-900">12px: The quick brown fox jumps</span>
		</div>
	</div>

	<!-- Test 4: Texture bleeding (dense repeated characters) -->
	<div class="bg-surface p-4 rounded-lg">
		<span class="text-sm text-ink-500 mb-2">Test 4: Texture Bleeding (no artifacts between letters)</span>
		<div class="flex flex-col gap-1 bg-white p-3 rounded mt-2">
			<span class="text-sm text-ink-900">iiiiiiiiiiiiiiiii llllllllllllllll</span>
			<span class="text-sm text-ink-900">jjjjjjjjjjjjjjjjj qqqqqqqqqqqqqqq</span>
			<span class="text-sm text-ink-900">ffffffffffffff yyyyyyyyyyyyyy</span>
			<span class="text-sm text-ink-900">Mixed: fijlqy fijlqy fijlqy fijlqy</span>
		</div>
	</div>

	<!-- Test 5: Different backgrounds -->
	<div class="flex flex-row gap-4">
		<div class="bg-ink-900 p-4 rounded-lg flex-1">
			<span class="text-sm text-ink-400 mb-2">Test 5a: Light on Dark</span>
			<div class="flex flex-col gap-1 mt-2">
				<span class="text-base text-white">Regular text</span>
				<span class="text-sm text-white">Small text: Alpine Linux</span>
				<span class="text-xs text-white">Tiny text: 0123456789</span>
			</div>
		</div>
		<div class="bg-white p-4 rounded-lg border border-border-light flex-1">
			<span class="text-sm text-ink-400 mb-2">Test 5b: Dark on Light</span>
			<div class="flex flex-col gap-1 mt-2">
				<span class="text-base text-ink-900">Regular text</span>
				<span class="text-sm text-ink-900">Small text: Alpine Linux</span>
				<span class="text-xs text-ink-900">Tiny text: 0123456789</span>
			</div>
		</div>
		<div class="bg-lime-500 p-4 rounded-lg flex-1">
			<span class="text-sm text-white mb-2">Test 5c: On Color</span>
			<div class="flex flex-col gap-1 mt-2">
				<span class="text-base text-white">Regular text</span>
				<span class="text-sm text-ink-900">Small text: Alpine Linux</span>
				<span class="text-xs text-ink-900">Tiny text: 0123456789</span>
			</div>
		</div>
	</div>

	<!-- Test 6: CrumbleCracker specific cases -->
	<div class="bg-surface p-4 rounded-lg">
		<span class="text-sm text-ink-500 mb-2">Test 6: CrumbleCracker UI Text</span>
		<div class="flex flex-col gap-2 mt-2">
			<span class="text-2xl text-ink-800">CrumbleCracker</span>
			<span class="text-base text-ink-900">Alpine Linux 3.20</span>
			<div class="flex flex-row gap-4">
				<span class="text-xs text-lime-600">Running</span>
				<span class="text-xs text-ink-400">Stopped</span>
				<span class="text-xs text-ink-500">2GB RAM</span>
				<span class="text-xs text-ink-500">4 vCPU</span>
			</div>
		</div>
	</div>

	<!-- Test 7: Full character set -->
	<div class="bg-surface p-4 rounded-lg">
		<span class="text-sm text-ink-500 mb-2">Test 7: Full Character Set</span>
		<div class="flex flex-col gap-1 bg-white p-3 rounded mt-2">
			<span class="text-sm text-ink-900">ABCDEFGHIJKLMNOPQRSTUVWXYZ</span>
			<span class="text-sm text-ink-900">abcdefghijklmnopqrstuvwxyz</span>
			<span class="text-sm text-ink-900">0123456789</span>
			<span class="text-sm text-ink-900">!@#$%^&*()_+-=[]{}|;':",./<>?</span>
		</div>
	</div>
</div>
`

func main() {
	output := flag.String("o", "screenshot.png", "output file path")
	width := flag.Int("w", 1200, "window width")
	height := flag.Int("h", 900, "window height")
	demo := flag.String("demo", "default", "demo to run: default, crumblecracker, fontdiag")
	noScreenshot := flag.Bool("no-screenshot", false, "do not take a screenshot")
	flag.Parse()

	if err := run(*output, *width, *height, *demo, *noScreenshot); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(output string, width, height int, demo string, noScreenshot bool) error {
	// Select demo HTML
	var htmlContent string
	var bgColor color.RGBA

	switch demo {
	case "crumblecracker":
		htmlContent = crumbleCrackerHTML
		bgColor = hex("#FFFBF7") // Cream canvas
	case "fontdiag":
		htmlContent = fontDiagnosticHTML
		bgColor = hex("#FFFFFF") // Pure white for font diagnostics
	default:
		htmlContent = kitchenSinkHTML
		bgColor = hex("#1a1b26") // Tokyo Night background
	}

	// Create window
	win, err := graphics.New("HTML Kitchen Sink", width, height)
	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	// Load text renderer
	textRenderer, err := text.Load(win)
	if err != nil {
		return fmt.Errorf("failed to create text renderer: %w", err)
	}

	// Parse HTML
	doc, err := html.Parse(htmlContent)
	if err != nil {
		return fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Register handlers (just for demo)
	doc.SetHandler("cancel", func() {
		fmt.Println("Cancel clicked")
	})
	doc.SetHandler("confirm", func() {
		fmt.Println("Confirm clicked")
	})

	// Render HTML to widget tree
	ctx := &html.RenderContext{
		Window:       win,
		TextRenderer: textRenderer,
	}
	widget := doc.Render(ctx)

	// Create root and set widget
	root := ui.NewRoot(textRenderer)
	root.SetChild(widget)

	// Setup window
	win.SetClear(true)
	win.SetClearColor(bgColor)

	frameCount := 0
	var screenshot error

	// Run render loop
	err = win.Loop(func(f graphics.Frame) error {
		// Render the UI
		root.DrawOnly(f)

		frameCount++

		if !noScreenshot {
			// Take screenshot on second frame (after first render completes)
			if frameCount == 2 {
				img, err := f.Screenshot()
				if err != nil {
					screenshot = fmt.Errorf("failed to take screenshot: %w", err)
					return screenshot
				}

				file, err := os.Create(output)
				if err != nil {
					screenshot = fmt.Errorf("failed to create output file: %w", err)
					return screenshot
				}
				defer file.Close()

				if err := png.Encode(file, img); err != nil {
					screenshot = fmt.Errorf("failed to encode PNG: %w", err)
					return screenshot
				}

				fmt.Printf("Screenshot saved to %s\n", output)
				return fmt.Errorf("done") // Exit the loop
			}
		}

		return nil
	})

	// "done" error is expected
	if err != nil && err.Error() == "done" {
		return screenshot
	}

	return err
}

// hex parses a CSS hex color.
func hex(s string) color.RGBA {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	var r, g, b uint8
	if len(s) == 6 {
		r = hexDigit(s[0])<<4 | hexDigit(s[1])
		g = hexDigit(s[2])<<4 | hexDigit(s[3])
		b = hexDigit(s[4])<<4 | hexDigit(s[5])
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func hexDigit(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
