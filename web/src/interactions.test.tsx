import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { SettingsPage, TemplatesPage, WelcomePage } from "./pages";

vi.mock("./api", () => ({
  api: {
    projects: vi.fn(async () => []),
    createProject: vi.fn(),
    settings: vi.fn(async () => []),
    drives: vi.fn(async () => ["C:\\"]),
    directories: vi.fn(async () => []),
    templates: vi.fn(async () => [
      {
        id: "task",
        kind: "task",
        name: "任务模板",
        description: "task",
        builtin: 1,
      },
      {
        id: "project",
        kind: "project",
        name: "项目模板示例",
        description: "project",
        builtin: 1,
      },
    ]),
  },
}));

function renderPage(page: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{page}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("关键页面交互", () => {
  it("点击添加项目中的浏览按钮会打开网页目录选择器", async () => {
    renderPage(<WelcomePage />);
    fireEvent.click(
      await screen.findByRole("button", { name: "添加本地项目" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "浏览" }));
    expect(
      screen.getByRole("heading", { name: "选择本地目录" }),
    ).toBeInTheDocument();
  });

  it("设置子页签会切换内容而不是停留在基本信息", async () => {
    renderPage(<SettingsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "执行与并发" }));
    expect(screen.queryByText("GENERAL")).not.toBeInTheDocument();
    expect(screen.getByText("EXECUTION")).toBeInTheDocument();
  });

  it("模板页签会过滤对应类型", async () => {
    renderPage(<TemplatesPage />);
    expect(
      await screen.findByText("任务模板", { selector: "h2" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "项目模板" }));
    expect(
      screen.queryByText("任务模板", { selector: "h2" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("项目模板示例")).toBeInTheDocument();
  });
});
