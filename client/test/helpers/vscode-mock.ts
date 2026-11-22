import Module from "module";
import * as path from "path";

export class MockEventEmitter<T> {
  private listeners: Array<(e: T) => unknown> = [];
  readonly event = (listener: (e: T) => unknown) => {
    this.listeners.push(listener);
    return {
      dispose: () => {
        const idx = this.listeners.indexOf(listener);
        if (idx >= 0) this.listeners.splice(idx, 1);
      },
    };
  };
  fire(data?: T): void {
    for (const l of [...this.listeners]) {
      l(data as T);
    }
  }
}

export class MockThemeColor {
  constructor(public readonly id: string) {}
}

export class MockThemeIcon {
  constructor(
    public readonly id: string,
    public readonly color?: MockThemeColor
  ) {}
}

export class MockTreeItem {
  id?: string;
  description?: string;
  tooltip?: string;
  iconPath?: MockThemeIcon;
  contextValue?: string;
  command?: { title: string; command: string; arguments?: unknown[] };
  constructor(
    public label: string,
    public collapsibleState: number = 0
  ) {}
}

export class MockUri {
  constructor(
    public readonly scheme: string,
    public readonly fsPath: string,
    public readonly raw?: string
  ) {}
  toString(): string {
    return this.raw || `${this.scheme}://${this.fsPath}`;
  }
  static file(fsPath: string): MockUri {
    return new MockUri("file", fsPath);
  }
  static parse(value: string): MockUri {
    if (value.startsWith("file://")) {
      return new MockUri("file", decodeURIComponent(value.slice("file://".length)), value);
    }
    return new MockUri("untitled", value, value);
  }
}

export class MockPosition {
  constructor(
    public readonly line: number,
    public readonly character: number
  ) {}
}

export class MockRange {
  constructor(
    public readonly start: MockPosition,
    public readonly end: MockPosition
  ) {}
}

export class MockWorkspaceEdit {
  readonly replacements: Array<{ uri: MockUri; range: MockRange; newText: string }> = [];
  replace(uri: MockUri, range: MockRange, newText: string): void {
    this.replacements.push({ uri, range, newText });
  }
}

export class MockProcessExecution {
  constructor(
    public readonly process: string,
    public readonly args: string[],
    public readonly options?: { cwd?: string; env?: Record<string, string> }
  ) {}
}

export class MockTask {
  constructor(
    public readonly definition: Record<string, unknown>,
    public readonly scope: number,
    public readonly name: string,
    public readonly source: string,
    public readonly execution: MockProcessExecution
  ) {}
}

export interface MockTerminal {
  name: string;
  shown: boolean[];
  sentTexts: string[];
  show(preserveFocus?: boolean): void;
  sendText(text: string): void;
}

export interface MockWebviewPanel {
  viewType: string;
  title: string;
  viewColumn: number;
  options: Record<string, unknown>;
  revealed: number[];
  disposed: boolean;
  webview: {
    html: string;
    onDidReceiveMessage(cb: (msg: unknown) => unknown): { dispose(): void };
    emitMessage(msg: unknown): Promise<void>;
  };
  reveal(col: number): void;
  onDidDispose(cb: () => void): { dispose(): void };
  dispose(): void;
}

export interface MockLanguageClientInstance {
  id: string;
  name: string;
  serverOptions: unknown;
  clientOptions: unknown;
  started: boolean;
  stopped: boolean;
  notifications: Array<{ method: string; params: unknown }>;
  requests: Array<{ method: string; params: unknown }>;
  start(): Promise<void>;
  stop(): Promise<void>;
  sendRequest(method: string, params: unknown): Promise<unknown>;
  sendNotification(method: string, params: unknown): void;
}

export interface MockState {
  config: Record<string, unknown>;
  workspaceFolders: Array<{ uri: MockUri; name: string }> | undefined;
  activeTextEditor: { document: { uri: MockUri } } | undefined;
  commands: Map<string, (...args: unknown[]) => unknown>;
  treeProviders: Map<string, unknown>;
  statusBars: Array<{
    alignment: number;
    priority?: number;
    command?: string;
    text: string;
    tooltip?: string;
    shown: boolean;
    disposed: boolean;
    show(): void;
    dispose(): void;
  }>;
  terminals: MockTerminal[];
  watchers: Array<{ glob: string; disposed: boolean; dispose(): void }>;
  webviews: MockWebviewPanel[];
  notifications: { errors: string[]; warnings: string[]; infos: string[] };
  openedDocs: Array<{ uri?: MockUri; language?: string; content?: string; lineCount: number; saved: boolean; save(): Promise<boolean> }>;
  shownDocs: Array<{ doc: unknown; options?: unknown; revealedRanges: MockRange[] }>;
  appliedEdits: MockWorkspaceEdit[];
  executedTasks: MockTask[];
  inputBoxQueue: Array<string | undefined>;
  quickPickQueue: Array<unknown>;
  configListeners: Array<(e: { affectsConfiguration: (section: string) => boolean }) => void>;
  saveListeners: Array<(doc: { uri: MockUri }) => void>;
  languageClients: MockLanguageClientInstance[];
  nextClientStartError: Error | null;
  requestHandler: (method: string, params: unknown) => Promise<unknown> | unknown;
}

export function createMockState(): MockState {
  return {
    config: {},
    workspaceFolders: undefined,
    activeTextEditor: undefined,
    commands: new Map(),
    treeProviders: new Map(),
    statusBars: [],
    terminals: [],
    watchers: [],
    webviews: [],
    notifications: { errors: [], warnings: [], infos: [] },
    openedDocs: [],
    shownDocs: [],
    appliedEdits: [],
    executedTasks: [],
    inputBoxQueue: [],
    quickPickQueue: [],
    configListeners: [],
    saveListeners: [],
    languageClients: [],
    nextClientStartError: null,
    requestHandler: async () => undefined,
  };
}

let activeState: MockState = createMockState();
let hookInstalled = false;

export function resetMockState(): MockState {
  activeState = createMockState();
  return activeState;
}

export function getMockState(): MockState {
  return activeState;
}

export function createMockExtensionContext(extensionPath: string) {
  const subscriptions: Array<{ dispose(): void }> = [];
  return {
    extensionPath,
    subscriptions,
    disposeAll() {
      for (const s of subscriptions) {
        s.dispose();
      }
      subscriptions.length = 0;
    },
  };
}

function buildVscodeMock() {
  return {
    EventEmitter: MockEventEmitter,
    TreeItem: MockTreeItem,
    TreeItemCollapsibleState: { None: 0, Collapsed: 1, Expanded: 2 },
    ThemeIcon: MockThemeIcon,
    ThemeColor: MockThemeColor,
    StatusBarAlignment: { Left: 1, Right: 2 },
    ViewColumn: { One: 1, Beside: -2 },
    TextEditorRevealType: { Default: 0 },
    TaskScope: { Global: 1, Workspace: 2 },
    Uri: MockUri,
    Position: MockPosition,
    Range: MockRange,
    WorkspaceEdit: MockWorkspaceEdit,
    ProcessExecution: MockProcessExecution,
    Task: MockTask,
    window: {
      get terminals() {
        return activeState.terminals;
      },
      get activeTextEditor() {
        return activeState.activeTextEditor;
      },
      createTerminal(name: string): MockTerminal {
        const term: MockTerminal = {
          name,
          shown: [],
          sentTexts: [],
          show(preserveFocus = false) {
            this.shown.push(preserveFocus);
          },
          sendText(text: string) {
            this.sentTexts.push(text);
          },
        };
        activeState.terminals.push(term);
        return term;
      },
      createStatusBarItem(alignment: number, priority?: number) {
        const sb = {
          alignment,
          priority,
          command: undefined as string | undefined,
          text: "",
          tooltip: undefined as string | undefined,
          shown: false,
          disposed: false,
          show() {
            this.shown = true;
          },
          dispose() {
            this.disposed = true;
          },
        };
        activeState.statusBars.push(sb);
        return sb;
      },
      registerTreeDataProvider(id: string, provider: unknown) {
        activeState.treeProviders.set(id, provider);
        return {
          dispose() {
            activeState.treeProviders.delete(id);
          },
        };
      },
      showErrorMessage(msg: string) {
        activeState.notifications.errors.push(msg);
        return Promise.resolve(undefined);
      },
      showWarningMessage(msg: string) {
        activeState.notifications.warnings.push(msg);
        return Promise.resolve(undefined);
      },
      showInformationMessage(msg: string) {
        activeState.notifications.infos.push(msg);
        return Promise.resolve(undefined);
      },
      async showInputBox(_options?: unknown) {
        return activeState.inputBoxQueue.shift();
      },
      async showQuickPick(items: unknown[], _options?: unknown) {
        const next = activeState.quickPickQueue.shift();
        if (typeof next === "function") {
          return (next as (items: unknown[]) => unknown)(items);
        }
        return next;
      },
      async showTextDocument(doc: unknown, options?: unknown) {
        const entry = { doc, options, revealedRanges: [] as MockRange[] };
        activeState.shownDocs.push(entry);
        return {
          document: doc,
          revealRange(range: MockRange) {
            entry.revealedRanges.push(range);
          },
        };
      },
      createWebviewPanel(viewType: string, title: string, viewColumn: number, options: Record<string, unknown>): MockWebviewPanel {
        const msgListeners: Array<(msg: unknown) => unknown> = [];
        const disposeListeners: Array<() => void> = [];
        const panel: MockWebviewPanel = {
          viewType,
          title,
          viewColumn,
          options,
          revealed: [],
          disposed: false,
          webview: {
            html: "",
            onDidReceiveMessage(cb: (msg: unknown) => unknown) {
              msgListeners.push(cb);
              return { dispose() {} };
            },
            async emitMessage(msg: unknown) {
              for (const cb of msgListeners) {
                await cb(msg);
              }
            },
          },
          reveal(col: number) {
            this.revealed.push(col);
          },
          onDidDispose(cb: () => void) {
            disposeListeners.push(cb);
            return { dispose() {} };
          },
          dispose() {
            this.disposed = true;
            for (const cb of disposeListeners) cb();
          },
        };
        activeState.webviews.push(panel);
        return panel;
      },
    },
    workspace: {
      get workspaceFolders() {
        return activeState.workspaceFolders;
      },
      getConfiguration(section?: string) {
        return {
          get<T>(key: string, defaultValue?: T): T | undefined {
            const fullKey = section ? `${section}.${key}` : key;
            if (Object.prototype.hasOwnProperty.call(activeState.config, fullKey)) {
              return activeState.config[fullKey] as T;
            }
            if (Object.prototype.hasOwnProperty.call(activeState.config, key)) {
              return activeState.config[key] as T;
            }
            return defaultValue;
          },
        };
      },
      createFileSystemWatcher(glob: string) {
        const w = {
          glob,
          disposed: false,
          dispose() {
            this.disposed = true;
          },
        };
        activeState.watchers.push(w);
        return w;
      },
      async openTextDocument(arg: MockUri | { language?: string; content?: string }) {
        const isUri = arg instanceof MockUri;
        const doc = {
          uri: isUri ? arg : undefined,
          language: isUri ? undefined : arg.language,
          content: isUri ? undefined : arg.content,
          lineCount: isUri ? 10 : (arg.content || "").split("\n").length,
          saved: false,
          async save() {
            this.saved = true;
            return true;
          },
        };
        activeState.openedDocs.push(doc);
        return doc;
      },
      async applyEdit(edit: MockWorkspaceEdit) {
        activeState.appliedEdits.push(edit);
        return true;
      },
      getWorkspaceFolder(uri: MockUri) {
        if (!activeState.workspaceFolders) return undefined;
        return activeState.workspaceFolders.find(
          (f) => uri.fsPath === f.uri.fsPath || uri.fsPath.startsWith(f.uri.fsPath + path.sep)
        );
      },
      onDidChangeConfiguration(cb: (e: { affectsConfiguration: (section: string) => boolean }) => void) {
        activeState.configListeners.push(cb);
        return {
          dispose() {
            const idx = activeState.configListeners.indexOf(cb);
            if (idx >= 0) activeState.configListeners.splice(idx, 1);
          },
        };
      },
      onDidSaveTextDocument(cb: (doc: { uri: MockUri }) => void) {
        activeState.saveListeners.push(cb);
        return {
          dispose() {
            const idx = activeState.saveListeners.indexOf(cb);
            if (idx >= 0) activeState.saveListeners.splice(idx, 1);
          },
        };
      },
    },
    commands: {
      registerCommand(name: string, cb: (...args: unknown[]) => unknown) {
        activeState.commands.set(name, cb);
        return {
          dispose() {
            activeState.commands.delete(name);
          },
        };
      },
      async executeCommand(name: string, ...args: unknown[]) {
        const fn = activeState.commands.get(name);
        if (fn) {
          return fn(...args);
        }
        return undefined;
      },
    },
    tasks: {
      async executeTask(task: MockTask) {
        activeState.executedTasks.push(task);
        return task;
      },
    },
  };
}

function buildLanguageClientMock() {
  class MockLanguageClient implements MockLanguageClientInstance {
    started = false;
    stopped = false;
    notifications: Array<{ method: string; params: unknown }> = [];
    requests: Array<{ method: string; params: unknown }> = [];

    constructor(
      public readonly id: string,
      public readonly name: string,
      public readonly serverOptions: unknown,
      public readonly clientOptions: unknown
    ) {
      activeState.languageClients.push(this);
    }

    async start(): Promise<void> {
      if (activeState.nextClientStartError) {
        const err = activeState.nextClientStartError;
        activeState.nextClientStartError = null;
        throw err;
      }
      this.started = true;
    }

    async stop(): Promise<void> {
      this.stopped = true;
    }

    async sendRequest(method: string, params: unknown): Promise<unknown> {
      this.requests.push({ method, params });
      return activeState.requestHandler(method, params);
    }

    sendNotification(method: string, params: unknown): void {
      this.notifications.push({ method, params });
    }
  }

  return { LanguageClient: MockLanguageClient };
}

export function installVscodeMock(): void {
  if (hookInstalled) return;
  hookInstalled = true;
  const vscodeMock = buildVscodeMock();
  const lcMock = buildLanguageClientMock();
  const mod = Module as unknown as {
    _load: (request: string, parent: unknown, isMain: boolean) => unknown;
  };
  const origLoad = mod._load;
  mod._load = function (request: string, parent: unknown, isMain: boolean) {
    if (request === "vscode") return vscodeMock;
    if (request === "vscode-languageclient/node") return lcMock;
    return origLoad.call(this, request, parent, isMain);
  };
}

export function withMockPlatform<T>(platform: NodeJS.Platform, fn: () => T): T {
  const desc = Object.getOwnPropertyDescriptor(process, "platform");
  Object.defineProperty(process, "platform", { value: platform, configurable: true });
  const restore = () => {
    if (desc) {
      Object.defineProperty(process, "platform", desc);
    }
  };
  try {
    const res = fn();
    if (res && typeof (res as unknown as Promise<unknown>).finally === "function") {
      return (res as unknown as Promise<unknown>).finally(restore) as unknown as T;
    }
    restore();
    return res;
  } catch (err) {
    restore();
    throw err;
  }
}

// Auto-install on import so subsequent hoisted imports of client/src modules resolve mocked vscode.
installVscodeMock();
