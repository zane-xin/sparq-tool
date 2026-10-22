package scaffold

const frontendPackageTemplate = `{
    "name": "@sparqjs/plugin-{{name}}-frontend",
    "private": true,
    "version": "0.1.0",
    "type": "module",
    "scripts": {
        "dev": "vite --host 0.0.0.0 --port 5175 --strictPort",
        "build": "vue-tsc --noEmit && vite build"
    },
    "dependencies": {
        "@sparqjs/plugin-sdk": "^0.2.2",
        "vue": "^3.5.35"
    },
    "devDependencies": {
        "@sparqjs/vite-plugin": "^0.2.2",
        "@vitejs/plugin-vue": "^6.0.0",
        "sass": "^1.101.0",
        "typescript": "^5.0.0",
        "vite": "^8.0.14",
        "vue-tsc": "^3.1.5"
    }
}
`

const frontendTSConfigTemplate = `{
    "compilerOptions": {
        "target": "ES2022",
        "module": "ESNext",
        "moduleResolution": "Bundler",
        "strict": true,
        "allowImportingTsExtensions": true,
        "composite": false,
        "declaration": false,
        "declarationMap": false,
        "types": ["vite/client"]
    },
    "include": ["src/**/*.ts", "src/**/*.vue", "vite.config.ts"]
}
`

const frontendViteConfigTemplate = `import { createSparqTrustedRoot } from '@sparqjs/vite-plugin'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig(({ command }) => ({
    base: command === 'serve' ? '/plugins/{{name}}/' : './',
    plugins: [createSparqTrustedRoot(), vue()],
    server: {
        host: '0.0.0.0',
        port: 5175,
        strictPort: true,
    },
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        manifest: true,
        lib: {
            entry: 'src/frontend.ts',
            formats: ['es'],
            fileName: () => 'frontend.mjs',
        },
    },
}))
`

const frontendIndexTemplate = `<!doctype html>
<html lang="zh-CN">
    <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>{{name}} 管理</title>
    </head>
    <body>
        <main id="app" aria-live="polite">正在连接 {{name}} 服务...</main>
        <script type="module" src="/src/main.ts"></script>
    </body>
</html>
`

const frontendBootstrapTemplate = `import { bootstrapSparqVuePlugin } from '@sparqjs/plugin-sdk/vue'
import type { Component } from 'vue'

export async function bootstrapPluginSurface(component: Component): Promise<void> {
    try {
        await bootstrapSparqVuePlugin({ component })
    } catch (error) {
        const root = document.querySelector<HTMLElement>('#app')
        if (root) {
            root.className = 'plugin-bootstrap-error'
            root.textContent = error instanceof Error ? error.message : String(error)
        }
    }
}
`

const frontendEntryTemplate = `import App from './App.vue'
import { bootstrapPluginSurface } from './bootstrap'
import './style.scss'

void bootstrapPluginSurface(App)
`

const frontendAdminExtensionTemplate = `import { defineFrontendPlugin } from '@sparqjs/plugin-sdk/runtime'

export default defineFrontendPlugin((_host) => {
    // 在这里注册需要进入宿主实例的可信贡献。
})
`

const frontendAppTemplate = `<script setup lang="ts">
import { useSparqPlugin } from '@sparqjs/plugin-sdk/vue'
import { computed, onMounted } from 'vue'

const runtime = useSparqPlugin()
const environment = computed(() => (runtime.context.dev.enabled ? '开发模式' : runtime.context.app.environment))

onMounted(async () => {
    await runtime.client.surface.setTitle(runtime.context.plugin.title)
})
</script>

<template>
    <main class="plugin-page">
        <header class="plugin-page__header">
            <div>
                <p class="plugin-page__code">{{ runtime.context.plugin.code }}</p>
                <h1>{{ runtime.context.plugin.title }}</h1>
            </div>
            <span class="plugin-page__environment">{{ environment }}</span>
        </header>
        <section class="plugin-page__content"></section>
    </main>
</template>
`

const frontendStyleTemplate = `:root {
    color: #1f2937;
    background: #f7f8fa;
    font-family: Inter, "PingFang SC", "Microsoft YaHei", sans-serif;
}

* {
    box-sizing: border-box;
}

body {
    margin: 0;
}

.plugin-page {
    min-height: 100vh;
    padding: 24px;
}

.plugin-page__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding-bottom: 20px;
    border-bottom: 1px solid #dfe3e8;
}

.plugin-page__header h1,
.plugin-page__code {
    margin: 0;
}

.plugin-page__header h1 {
    margin-top: 4px;
    font-size: 22px;
    letter-spacing: 0;
}

.plugin-page__code,
.plugin-page__environment {
    color: #667085;
    font-size: 13px;
}

.plugin-page__content {
    min-height: 320px;
    padding-top: 24px;
}

.plugin-bootstrap-error {
    padding: 24px;
    color: #b42318;
    white-space: pre-wrap;
}
`

const frontendReadmeTemplate = `# @sparqjs/plugin-{{name}}-frontend

该目录提供插件统一的 Admin 前端入口。生产地址固定为 ` + "`/plugins/{{name}}/`" + `，页面通过 ` + "`@sparqjs/plugin-sdk`" + ` 获取宿主上下文和能力。

## 开发

` + "```bash" + `
pnpm dev
` + "```" + `

## 构建

` + "```bash" + `
sparq-tool plugin build
` + "```" + `

逻辑页面通过 Manifest Entry 和 Bridge Context 区分，不为每个页面创建独立 HTML 地址。
宿主页面和其他可信扩展统一写在 ` + "`src/frontend.ts`" + `。` + "`createSparqTrustedRoot()`" + ` 同时处理开发态 HMR、CSS 和生产构建描述；` + "`sparq-tool`" + ` 会校验最终文件并生成 ` + "`frontend/artifact.json`" + `。
`
