import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ChevronRight, Folder, HardDrive, Home } from "lucide-react";
import { useMemo, useState } from "react";
import { api } from "./api";
import { Button, Modal } from "./components";

type Props = {
  open: boolean;
  initialPath?: string;
  onClose: () => void;
  onSelect: (path: string) => void;
};

export function DirectoryPicker({
  open,
  initialPath = "",
  onClose,
  onSelect,
}: Props) {
  const [path, setPath] = useState(initialPath.trim());
  const drives = useQuery({
    queryKey: ["filesystem", "drives"],
    queryFn: api.drives,
    enabled: open,
  });
  const directories = useQuery({
    queryKey: ["filesystem", "directories", path],
    queryFn: () => api.directories(path),
    enabled: open && !!path,
  });
  const parent = useMemo(() => parentPath(path), [path]);
  const choose = (value: string) => {
    onSelect(value);
    onClose();
  };
  return (
    <Modal open={open} onClose={onClose} title="选择本地目录">
      <div className="directory-picker">
        <div className="directory-location">
          <Button
            aria-label="返回上级目录"
            disabled={!parent}
            onClick={() => parent && setPath(parent)}
          >
            <ArrowLeft />
          </Button>
          <button onClick={() => setPath("")}>
            <Home />
            此电脑
          </button>
          {path && (
            <>
              <ChevronRight />
              <code>{path}</code>
            </>
          )}
        </div>
        <div className="directory-list">
          {!path &&
            (drives.data || []).map((drive) => (
              <button key={drive} onClick={() => setPath(drive)}>
                <HardDrive />
                <span>
                  <b>{drive}</b>
                  <small>本地磁盘</small>
                </span>
                <ChevronRight />
              </button>
            ))}
          {path && directories.isLoading && (
            <p className="muted">正在读取目录…</p>
          )}
          {path && directories.error && (
            <p className="form-error">{directories.error.message}</p>
          )}
          {path &&
            !directories.isLoading &&
            !directories.error &&
            directories.data?.map((entry) => (
              <button key={entry.path} onClick={() => setPath(entry.path)}>
                <Folder />
                <span>
                  <b>{entry.name}</b>
                  <small>{entry.path}</small>
                </span>
                <ChevronRight />
              </button>
            ))}
          {path &&
            !directories.isLoading &&
            !directories.error &&
            !directories.data?.length && (
              <p className="muted">这个目录中没有子目录。</p>
            )}
        </div>
        <footer>
          <span>{path || "请选择一个磁盘"}</span>
          <Button onClick={onClose}>取消</Button>
          <Button
            variant="primary"
            disabled={!path}
            onClick={() => choose(path)}
          >
            选择此目录
          </Button>
        </footer>
      </div>
    </Modal>
  );
}

function parentPath(path: string) {
  const normalized = path.replace(/[\\/]+$/, "");
  if (!normalized || /^[A-Za-z]:$/.test(normalized) || normalized === "/")
    return "";
  const index = Math.max(
    normalized.lastIndexOf("\\"),
    normalized.lastIndexOf("/"),
  );
  if (index < 0) return "";
  const result = normalized.slice(0, index);
  return /^[A-Za-z]:$/.test(result) ? result + "\\" : result || "/";
}
