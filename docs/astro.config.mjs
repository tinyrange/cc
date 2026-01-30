// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://tinyrange.github.io',
	base: "/cc",
	integrations: [
		starlight({
			title: 'CrumbleCracker',
			logo: {
				light: './src/assets/logo-dark.svg',
				dark: './src/assets/logo.svg',
			},
			customCss: ['./src/styles/custom.css'],
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/tinyrange/cc' }],
			sidebar: [
				{
					label: 'Getting Started',
					items: [
						{ label: 'Introduction', slug: 'getting-started/introduction' },
						{ label: 'Installation', slug: 'getting-started/installation' },
						{ label: 'Quick Start', slug: 'getting-started/quick-start' },
					],
				},
				{
					label: 'CrumbleCracker App',
					items: [
						{ label: 'Overview', slug: 'app/overview' },
						{ label: 'Creating VMs', slug: 'app/creating-vms' },
						{ label: 'Terminal Mode', slug: 'app/terminal-mode' },
						{ label: 'Bundles', slug: 'app/bundles' },
						{ label: 'Settings', slug: 'app/settings' },
					],
				},
				{
					label: 'Go API',
					items: [
						{ label: 'Overview', slug: 'api/overview' },
						{ label: 'Creating Instances', slug: 'api/creating-instances' },
						{ label: 'Filesystem', slug: 'api/filesystem' },
						{ label: 'Commands', slug: 'api/commands' },
						{ label: 'Networking', slug: 'api/networking' },
						{ label: 'OCI Images', slug: 'api/oci-images' },
						{ label: 'Snapshots', slug: 'api/snapshots' },
						{ label: 'Dockerfile', slug: 'api/dockerfile' },
						{ label: 'GPU Support', slug: 'api/gpu' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'Options', slug: 'reference/options' },
						{ label: 'Environment Variables', slug: 'reference/environment' },
						{ label: 'Errors', slug: 'reference/errors' },
					],
				},
			],
		}),
	],
});
