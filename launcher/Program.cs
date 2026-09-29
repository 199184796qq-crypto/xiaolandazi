using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Forms;

namespace LiveCompanionLauncher
{
    internal sealed class ServiceDefinition
    {
        public string Name;
        public string DisplayName;
        public string Command;
        public string Arguments;
        public string WorkingDirectory;
        public int Port;
        public string WebsiteUrl;
        public string StdoutLog;
        public string StderrLog;
        public bool Running;
        public int Pid;
        public int WrapperPid;
    }

    internal sealed class ServiceSnapshot
    {
        public bool Running;
        public int Pid;
    }

    internal sealed class MainForm : Form
    {
        private readonly string root;
        private readonly List<ServiceDefinition> services = new List<ServiceDefinition>();
        private readonly ListBox serviceList = new ListBox();
        private readonly Label selectedTitle = new Label();
        private readonly Label statusValue = new Label();
        private readonly Label pidValue = new Label();
        private readonly Label portValue = new Label();
        private readonly LinkLabel websiteValue = new LinkLabel();
        private readonly Label pathValue = new Label();
        private readonly RichTextBox logBox = new RichTextBox();
        private readonly Button startButton = new Button();
        private readonly Button stopButton = new Button();
        private readonly Button restartButton = new Button();
        private readonly Button openFolderButton = new Button();
        private readonly Button startAllButton = new Button();
        private readonly Button stopAllButton = new Button();
        private readonly Button exitButton = new Button();
        private readonly NotifyIcon trayIcon = new NotifyIcon();
        private readonly System.Windows.Forms.Timer uiTimer = new System.Windows.Forms.Timer();
        private readonly object statusLock = new object();
        private bool statusRefreshRunning;
        private bool allowClose;
        private string lastLogText = "";

        private readonly Color Navy = Color.FromArgb(31, 45, 71);
        private readonly Color Muted = Color.FromArgb(122, 135, 158);
        private readonly Color Accent = Color.FromArgb(84, 101, 214);
        private readonly Color Good = Color.FromArgb(31, 153, 98);
        private readonly Color Bad = Color.FromArgb(205, 76, 86);
        private readonly Color Panel = Color.FromArgb(247, 249, 253);

        public MainForm()
        {
            root = ResolveProjectRoot();
            LoadProcessEnvironment();
            BuildServiceDefinitions();
            BuildUi();
            BuildTray();

            uiTimer.Interval = 1200;
            uiTimer.Tick += delegate
            {
                RefreshStatusesAsync();
                RefreshSelectedLog();
            };
            uiTimer.Start();

            Shown += delegate
            {
                WriteLauncherLog("launcher shown; taking service control");
                TakeControlFromSupervisor();
                Task.Run(delegate
                {
                    Thread.Sleep(700);
                    BeginInvoke((Action)delegate { StartAllServices(); });
                });
            };
        }

        private static string ResolveProjectRoot()
        {
            string current = AppDomain.CurrentDomain.BaseDirectory.TrimEnd(Path.DirectorySeparatorChar);
            if (File.Exists(Path.Combine(current, "configs", "supervisor.json")))
                return current;
            DirectoryInfo parent = Directory.GetParent(current);
            if (parent != null && File.Exists(Path.Combine(parent.FullName, "configs", "supervisor.json")))
                return parent.FullName;
            return current;
        }

        private void LoadProcessEnvironment()
        {
            string envFile = Path.Combine(root, "configs", "cloud-dev.local");
            if (File.Exists(envFile))
            {
                foreach (string raw in File.ReadAllLines(envFile, Encoding.UTF8))
                {
                    string line = raw.Trim();
                    if (line.Length == 0 || line.StartsWith("#")) continue;
                    int idx = raw.IndexOf('=');
                    if (idx <= 0) continue;
                    string name = raw.Substring(0, idx).Trim();
                    string value = raw.Substring(idx + 1);
                    if (name.Length > 0)
                        Environment.SetEnvironmentVariable(name, value, EnvironmentVariableTarget.Process);
                }
            }
            Environment.SetEnvironmentVariable(
                "MGMT_AVATAR_DIR",
                Path.Combine(root, "data", "avatars"),
                EnvironmentVariableTarget.Process
            );
        }

        private string NodeExe()
        {
            string common = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles),
                "nodejs",
                "node.exe"
            );
            return File.Exists(common) ? common : "node.exe";
        }

        private void BuildServiceDefinitions()
        {
            string logDir = Path.Combine(root, "data", "logs");
            Directory.CreateDirectory(logDir);
            string node = NodeExe();

            services.Add(ViteService("web-console", "Web 管理端", node, "web-console", 5173, "web-console"));
            services.Add(new ServiceDefinition
            {
                Name = "management-service",
                DisplayName = "Management 管理服务",
                Command = Path.Combine(root, "management-service", "bin", "management-service.exe"),
                Arguments = "",
                WorkingDirectory = root,
                Port = 8080,
                StdoutLog = Path.Combine(logDir, "management-service.stdout.log"),
                StderrLog = Path.Combine(logDir, "management-service.stderr.log")
            });
            services.Add(new ServiceDefinition
            {
                Name = "core-service",
                DisplayName = "Core 核心服务",
                Command = Path.Combine(root, "core-service", "bin", "core-service.exe"),
                Arguments = "",
                WorkingDirectory = root,
                Port = 8081,
                StdoutLog = Path.Combine(logDir, "core-service.stdout.log"),
                StderrLog = Path.Combine(logDir, "core-service.stderr.log")
            });
            services.Add(ViteService("customer-mobile", "客户手机端", node, "customer-mobile", 5174, "customer-mobile"));
            services.Add(ViteService("sales-mobile", "销售手机端", node, "sales-mobile", 5175, "sales-mobile"));
            services.Add(ViteService("device-simulator", "设备模拟器", node, "device-simulator", 5176, "device-simulator"));
        }

        private ServiceDefinition ViteService(
            string name,
            string displayName,
            string node,
            string folder,
            int port,
            string logBase)
        {
            return new ServiceDefinition
            {
                Name = name,
                DisplayName = displayName,
                Command = node,
                Arguments = Quote(Path.Combine(root, folder, "node_modules", "vite", "bin", "vite.js"))
                    + " --host 127.0.0.1 --port " + port,
                WorkingDirectory = Path.Combine(root, folder),
                Port = port,
                WebsiteUrl = "http://127.0.0.1:" + port,
                StdoutLog = Path.Combine(root, "data", "logs", logBase + ".stdout.log"),
                StderrLog = Path.Combine(root, "data", "logs", logBase + ".stderr.log")
            };
        }

        private void BuildUi()
        {
            Text = "小蓝直播搭子 · 服务启动器";
            Width = 1360;
            Height = 760;
            MinimumSize = new Size(1120, 620);
            StartPosition = FormStartPosition.CenterScreen;
            BackColor = Color.White;
            Font = new Font("Microsoft YaHei UI", 10F, FontStyle.Regular, GraphicsUnit.Point);

            FormClosing += OnFormClosing;
            Resize += delegate
            {
                if (WindowState == FormWindowState.Minimized)
                    MinimizeToTray();
            };

            Panel top = new Panel();
            top.Dock = DockStyle.Top;
            top.Height = 78;
            top.BackColor = Color.White;
            Controls.Add(top);

            Label title = new Label();
            title.Text = "小蓝直播搭子  ·  服务启动器";
            title.Font = new Font("Microsoft YaHei UI", 19F, FontStyle.Bold);
            title.ForeColor = Navy;
            title.AutoSize = true;
            title.Location = new Point(22, 18);
            top.Controls.Add(title);

            Label subtitle = new Label();
            subtitle.Text = "启动器接管服务时不显示 CMD 窗口；关闭窗口只会进入系统托盘";
            subtitle.ForeColor = Muted;
            subtitle.AutoSize = true;
            subtitle.Location = new Point(25, 52);
            top.Controls.Add(subtitle);

            ConfigureTopButton(exitButton, "退出", 84, Color.FromArgb(249, 244, 245), Color.FromArgb(168, 70, 80));
            exitButton.Anchor = AnchorStyles.Top | AnchorStyles.Right;
            exitButton.Location = new Point(Width - 122, 20);
            exitButton.Click += delegate { ExitLauncher(); };
            top.Controls.Add(exitButton);

            ConfigureTopButton(stopAllButton, "全部停止", 96, Color.FromArgb(247, 248, 251), Navy);
            stopAllButton.Anchor = AnchorStyles.Top | AnchorStyles.Right;
            stopAllButton.Location = new Point(Width - 226, 20);
            stopAllButton.Click += delegate { StopAllServices(); };
            top.Controls.Add(stopAllButton);

            ConfigureTopButton(startAllButton, "全部启动", 96, Accent, Color.White);
            startAllButton.Anchor = AnchorStyles.Top | AnchorStyles.Right;
            startAllButton.Location = new Point(Width - 330, 20);
            startAllButton.Click += delegate { StartAllServices(); };
            top.Controls.Add(startAllButton);

            SplitContainer split = new SplitContainer();
            split.Dock = DockStyle.Fill;
            split.SplitterDistance = 390;
            split.FixedPanel = FixedPanel.Panel1;
            split.BackColor = Color.FromArgb(227, 232, 242);
            Controls.Add(split);
            split.BringToFront();

            BuildLeftPanel(split.Panel1);
            BuildRightPanel(split.Panel2);

            if (services.Count > 0)
                serviceList.SelectedIndex = 0;
        }

        private void ConfigureTopButton(Button button, string text, int width, Color back, Color fore)
        {
            button.Text = text;
            button.Width = width;
            button.Height = 38;
            button.FlatStyle = FlatStyle.Flat;
            button.FlatAppearance.BorderSize = 0;
            button.BackColor = back;
            button.ForeColor = fore;
            button.Font = new Font(Font, FontStyle.Bold);
            button.Cursor = Cursors.Hand;
        }

        private void BuildLeftPanel(Control host)
        {
            host.BackColor = Panel;
            Panel header = new Panel();
            header.Dock = DockStyle.Top;
            header.Height = 70;
            header.BackColor = Panel;
            host.Controls.Add(header);

            Label label = new Label();
            label.Text = "服务列表";
            label.Font = new Font("Microsoft YaHei UI", 16F, FontStyle.Bold);
            label.ForeColor = Navy;
            label.AutoSize = true;
            label.Location = new Point(20, 18);
            header.Controls.Add(label);

            Label small = new Label();
            small.Text = services.Count + " 个主服务";
            small.ForeColor = Muted;
            small.AutoSize = true;
            small.Anchor = AnchorStyles.Top | AnchorStyles.Right;
            small.Location = new Point(292, 24);
            header.Controls.Add(small);

            serviceList.Dock = DockStyle.Fill;
            serviceList.BorderStyle = BorderStyle.None;
            serviceList.BackColor = Panel;
            serviceList.DrawMode = DrawMode.OwnerDrawFixed;
            serviceList.ItemHeight = 72;
            serviceList.IntegralHeight = false;
            serviceList.Items.AddRange(services.Cast<object>().ToArray());
            serviceList.DrawItem += DrawServiceItem;
            serviceList.SelectedIndexChanged += delegate
            {
                lastLogText = "";
                RefreshSelectedDetails();
                RefreshSelectedLog();
            };
            host.Controls.Add(serviceList);
            serviceList.BringToFront();
        }

        private void DrawServiceItem(object sender, DrawItemEventArgs e)
        {
            if (e.Index < 0 || e.Index >= services.Count) return;
            ServiceDefinition service = services[e.Index];
            bool selected = (e.State & DrawItemState.Selected) == DrawItemState.Selected;
            Color bg = selected ? Color.FromArgb(234, 238, 255) : Panel;
            using (SolidBrush brush = new SolidBrush(bg))
                e.Graphics.FillRectangle(brush, e.Bounds);

            Rectangle card = new Rectangle(e.Bounds.X + 10, e.Bounds.Y + 5, e.Bounds.Width - 20, e.Bounds.Height - 10);
            using (Pen pen = new Pen(selected ? Color.FromArgb(139, 151, 230) : Color.FromArgb(225, 230, 239)))
                e.Graphics.DrawRectangle(pen, card);

            Color dotColor = service.Running ? Good : Bad;
            using (SolidBrush dot = new SolidBrush(dotColor))
                e.Graphics.FillEllipse(dot, e.Bounds.X + 25, e.Bounds.Y + 25, 11, 11);

            using (Font bold = new Font(Font, FontStyle.Bold))
            using (SolidBrush nameBrush = new SolidBrush(Navy))
                e.Graphics.DrawString(service.DisplayName, bold, nameBrush, e.Bounds.X + 48, e.Bounds.Y + 15);
            using (Font small = new Font(Font.FontFamily, 9F))
            using (SolidBrush stateBrush = new SolidBrush(service.Running ? Good : Muted))
                e.Graphics.DrawString(
                    service.Running ? "运行中  ·  " + service.Port : "已停止  ·  " + service.Port,
                    small,
                    stateBrush,
                    e.Bounds.X + 48,
                    e.Bounds.Y + 40
                );
        }

        private void BuildRightPanel(Control host)
        {
            host.BackColor = Color.White;

            Panel info = new Panel();
            info.Dock = DockStyle.Top;
            info.Height = 228;
            info.BackColor = Color.White;
            host.Controls.Add(info);

            selectedTitle.Text = "服务";
            selectedTitle.Font = new Font("Microsoft YaHei UI", 18F, FontStyle.Bold);
            selectedTitle.ForeColor = Navy;
            selectedTitle.AutoSize = true;
            selectedTitle.Location = new Point(24, 18);
            info.Controls.Add(selectedTitle);

            AddInfoRow(info, "运行状态", statusValue, 62);
            AddInfoRow(info, "PID", pidValue, 91);
            AddInfoRow(info, "端口", portValue, 120);
            AddInfoLinkRow(info, "网站地址", websiteValue, 149);
            AddInfoRow(info, "位置", pathValue, 178);
            pathValue.AutoEllipsis = true;
            pathValue.Width = 660;
            websiteValue.LinkClicked += delegate
            {
                ServiceDefinition service = SelectedService();
                if (service == null || string.IsNullOrWhiteSpace(service.WebsiteUrl)) return;
                try
                {
                    Process.Start(new ProcessStartInfo(service.WebsiteUrl) { UseShellExecute = true });
                }
                catch { }
            };

            FlowLayoutPanel actions = new FlowLayoutPanel();
            actions.FlowDirection = FlowDirection.LeftToRight;
            actions.WrapContents = false;
            actions.AutoSize = true;
            actions.Anchor = AnchorStyles.Top | AnchorStyles.Right;
            actions.Location = new Point(510, 20);
            info.Controls.Add(actions);

            ConfigureActionButton(startButton, "启动", Accent, Color.White);
            ConfigureActionButton(stopButton, "停止", Color.FromArgb(245, 247, 251), Navy);
            ConfigureActionButton(restartButton, "重启", Color.FromArgb(245, 247, 251), Navy);
            ConfigureActionButton(openFolderButton, "打开位置", Color.FromArgb(245, 247, 251), Navy);
            actions.Controls.Add(startButton);
            actions.Controls.Add(stopButton);
            actions.Controls.Add(restartButton);
            actions.Controls.Add(openFolderButton);

            startButton.Click += delegate { WithSelected(StartService); };
            stopButton.Click += delegate { WithSelected(StopService); };
            restartButton.Click += delegate { WithSelected(RestartService); };
            openFolderButton.Click += delegate { WithSelected(OpenServiceFolder); };

            Panel logHeader = new Panel();
            logHeader.Dock = DockStyle.Top;
            logHeader.Height = 48;
            logHeader.BackColor = Color.FromArgb(249, 250, 253);
            host.Controls.Add(logHeader);
            logHeader.BringToFront();

            Label logTitle = new Label();
            logTitle.Text = "实时日志";
            logTitle.ForeColor = Navy;
            logTitle.Font = new Font(Font, FontStyle.Bold);
            logTitle.AutoSize = true;
            logTitle.Location = new Point(22, 14);
            logHeader.Controls.Add(logTitle);

            Label logHint = new Label();
            logHint.Text = "自动读取 data\\logs 中对应日志";
            logHint.ForeColor = Muted;
            logHint.AutoSize = true;
            logHint.Location = new Point(95, 14);
            logHeader.Controls.Add(logHint);

            logBox.Dock = DockStyle.Fill;
            logBox.BorderStyle = BorderStyle.None;
            logBox.BackColor = Color.FromArgb(24, 31, 45);
            logBox.ForeColor = Color.FromArgb(221, 228, 239);
            logBox.Font = new Font("Consolas", 9.5F);
            logBox.ReadOnly = true;
            logBox.WordWrap = false;
            logBox.DetectUrls = false;
            host.Controls.Add(logBox);
            logBox.BringToFront();
        }

        private void AddInfoRow(Control host, string caption, Label value, int y)
        {
            Label key = new Label();
            key.Text = caption;
            key.ForeColor = Muted;
            key.Width = 80;
            key.Location = new Point(25, y);
            host.Controls.Add(key);

            value.Text = "-";
            value.ForeColor = Navy;
            value.Location = new Point(110, y);
            value.AutoSize = true;
            host.Controls.Add(value);
        }

        private void AddInfoLinkRow(Control host, string caption, LinkLabel value, int y)
        {
            Label key = new Label();
            key.Text = caption;
            key.ForeColor = Muted;
            key.Width = 80;
            key.Location = new Point(25, y);
            host.Controls.Add(key);

            value.Text = "-";
            value.LinkColor = Accent;
            value.ActiveLinkColor = Color.FromArgb(69, 84, 187);
            value.VisitedLinkColor = Accent;
            value.Location = new Point(110, y);
            value.AutoSize = true;
            value.Cursor = Cursors.Hand;
            host.Controls.Add(value);
        }

        private void ConfigureActionButton(Button button, string text, Color back, Color fore)
        {
            button.Text = text;
            button.AutoSize = true;
            button.MinimumSize = new Size(74, 36);
            button.Margin = new Padding(0, 0, 8, 0);
            button.FlatStyle = FlatStyle.Flat;
            button.FlatAppearance.BorderSize = 0;
            button.BackColor = back;
            button.ForeColor = fore;
            button.Font = new Font(Font, FontStyle.Bold);
            button.Cursor = Cursors.Hand;
        }

        private void BuildTray()
        {
            trayIcon.Text = "小蓝直播搭子 · 服务启动器";
            trayIcon.Icon = SystemIcons.Application;
            trayIcon.Visible = true;
            trayIcon.DoubleClick += delegate { RestoreFromTray(); };

            ContextMenuStrip menu = new ContextMenuStrip();
            menu.Items.Add("显示启动器", null, delegate { RestoreFromTray(); });
            menu.Items.Add("全部启动", null, delegate { StartAllServices(); });
            menu.Items.Add(new ToolStripSeparator());
            menu.Items.Add("退出启动器", null, delegate { ExitLauncher(); });
            trayIcon.ContextMenuStrip = menu;
        }

        private ServiceDefinition SelectedService()
        {
            return serviceList.SelectedItem as ServiceDefinition;
        }

        private void WithSelected(Action<ServiceDefinition> action)
        {
            ServiceDefinition service = SelectedService();
            if (service != null) action(service);
        }

        private void RefreshSelectedDetails()
        {
            ServiceDefinition service = SelectedService();
            if (service == null) return;
            selectedTitle.Text = service.DisplayName;
            statusValue.Text = service.Running ? "运行中" : "已停止";
            statusValue.ForeColor = service.Running ? Good : Bad;
            pidValue.Text = service.Pid > 0 ? service.Pid.ToString() : "-";
            portValue.Text = service.Port.ToString();
            websiteValue.Text = string.IsNullOrWhiteSpace(service.WebsiteUrl) ? "-" : service.WebsiteUrl;
            websiteValue.Enabled = !string.IsNullOrWhiteSpace(service.WebsiteUrl);
            websiteValue.Cursor = websiteValue.Enabled ? Cursors.Hand : Cursors.Default;
            pathValue.Text = service.WorkingDirectory;
            startButton.Enabled = !service.Running;
            stopButton.Enabled = service.Running;
            restartButton.Enabled = service.Running;
        }

        private void RefreshStatusesAsync()
        {
            lock (statusLock)
            {
                if (statusRefreshRunning) return;
                statusRefreshRunning = true;
            }

            Task.Run(delegate
            {
                Dictionary<int, int> pids = GetListeningPids();
                Dictionary<ServiceDefinition, ServiceSnapshot> snapshots = new Dictionary<ServiceDefinition, ServiceSnapshot>();
                foreach (ServiceDefinition service in services)
                {
                    bool running = IsPortOpen(service.Port, 250);
                    int pid = pids.ContainsKey(service.Port) ? pids[service.Port] : 0;
                    snapshots[service] = new ServiceSnapshot { Running = running, Pid = pid };
                }
                BeginInvoke((Action)delegate
                {
                    foreach (KeyValuePair<ServiceDefinition, ServiceSnapshot> pair in snapshots)
                    {
                        pair.Key.Running = pair.Value.Running;
                        pair.Key.Pid = pair.Value.Pid;
                    }
                    serviceList.Invalidate();
                    RefreshSelectedDetails();
                    lock (statusLock) statusRefreshRunning = false;
                });
            });
        }

        private static bool IsPortOpen(int port, int timeoutMs)
        {
            try
            {
                using (TcpClient client = new TcpClient())
                {
                    IAsyncResult result = client.BeginConnect("127.0.0.1", port, null, null);
                    bool success = result.AsyncWaitHandle.WaitOne(timeoutMs);
                    if (!success) return false;
                    client.EndConnect(result);
                    return true;
                }
            }
            catch
            {
                return false;
            }
        }

        private static Dictionary<int, int> GetListeningPids()
        {
            Dictionary<int, int> result = new Dictionary<int, int>();
            try
            {
                ProcessStartInfo psi = new ProcessStartInfo("netstat.exe", "-ano -p tcp");
                psi.UseShellExecute = false;
                psi.CreateNoWindow = true;
                psi.RedirectStandardOutput = true;
                psi.RedirectStandardError = true;
                using (Process process = Process.Start(psi))
                {
                    string text = process.StandardOutput.ReadToEnd();
                    process.WaitForExit(2500);
                    foreach (string raw in text.Split(new[] { '\r', '\n' }, StringSplitOptions.RemoveEmptyEntries))
                    {
                        string line = raw.Trim();
                        if (!line.Contains("LISTENING") && !line.Contains("侦听")) continue;
                        string[] parts = line.Split(new[] { ' ', '\t' }, StringSplitOptions.RemoveEmptyEntries);
                        if (parts.Length < 5) continue;
                        string local = parts[1];
                        int colon = local.LastIndexOf(':');
                        if (colon < 0) continue;
                        int port;
                        int pid;
                        if (int.TryParse(local.Substring(colon + 1), out port)
                            && int.TryParse(parts[parts.Length - 1], out pid))
                            result[port] = pid;
                    }
                }
            }
            catch { }
            return result;
        }

        private void StartAllServices()
        {
            foreach (ServiceDefinition service in services)
            {
                if (!IsPortOpen(service.Port, 120))
                {
                    StartService(service);
                    Thread.Sleep(160);
                }
            }
            RefreshStatusesAsync();
        }

        private void StopAllServices()
        {
            if (MessageBox.Show(
                this,
                "确定停止全部服务吗？启动器仍会保留在界面中，可再一键启动。",
                "停止全部服务",
                MessageBoxButtons.YesNo,
                MessageBoxIcon.Question
            ) != DialogResult.Yes) return;

            foreach (ServiceDefinition service in services.AsEnumerable().Reverse())
                StopService(service);
            RefreshStatusesAsync();
        }

        private void StartService(ServiceDefinition service)
        {
            if (IsPortOpen(service.Port, 150))
            {
                service.Running = true;
                return;
            }
            try
            {
                Directory.CreateDirectory(Path.GetDirectoryName(service.StdoutLog));
                string commandLine = Quote(service.Command)
                    + (string.IsNullOrWhiteSpace(service.Arguments) ? "" : " " + service.Arguments)
                    + " >> " + Quote(service.StdoutLog)
                    + " 2>> " + Quote(service.StderrLog);

                ProcessStartInfo psi = new ProcessStartInfo("cmd.exe");
                psi.Arguments = "/d /s /c " + Quote(commandLine);
                psi.WorkingDirectory = service.WorkingDirectory;
                psi.UseShellExecute = false;
                psi.CreateNoWindow = true;
                psi.WindowStyle = ProcessWindowStyle.Hidden;
                Process wrapper = Process.Start(psi);
                service.WrapperPid = wrapper != null ? wrapper.Id : 0;
                WriteLauncherLog("start " + service.Name + " wrapper_pid=" + service.WrapperPid);
            }
            catch (Exception ex)
            {
                WriteLauncherLog("start failed " + service.Name + ": " + ex.Message);
                MessageBox.Show(this, "启动失败：\r\n" + ex.Message, service.DisplayName, MessageBoxButtons.OK, MessageBoxIcon.Error);
            }
            RefreshStatusesAsync();
        }

        private void StopService(ServiceDefinition service)
        {
            Dictionary<int, int> pids = GetListeningPids();
            int pid = pids.ContainsKey(service.Port) ? pids[service.Port] : service.Pid;
            if (pid > 0)
            {
                RunHidden("taskkill.exe", "/PID " + pid + " /T /F", 6000);
                WriteLauncherLog("stop " + service.Name + " pid=" + pid);
            }
            else if (service.WrapperPid > 0)
            {
                RunHidden("taskkill.exe", "/PID " + service.WrapperPid + " /T /F", 6000);
                WriteLauncherLog("stop wrapper " + service.Name + " pid=" + service.WrapperPid);
            }
            service.Pid = 0;
            service.WrapperPid = 0;
            service.Running = false;
            serviceList.Invalidate();
            RefreshSelectedDetails();
        }

        private void RestartService(ServiceDefinition service)
        {
            StopService(service);
            Task.Run(delegate
            {
                Thread.Sleep(900);
                BeginInvoke((Action)delegate { StartService(service); });
            });
        }

        private void OpenServiceFolder(ServiceDefinition service)
        {
            try
            {
                Process.Start("explorer.exe", Quote(service.WorkingDirectory));
            }
            catch { }
        }

        private void RefreshSelectedLog()
        {
            ServiceDefinition service = SelectedService();
            if (service == null) return;
            string output = ReadTail(service.StdoutLog, 70000);
            string error = ReadTail(service.StderrLog, 35000);
            StringBuilder builder = new StringBuilder();
            builder.AppendLine("[" + service.DisplayName + "]  STDOUT");
            builder.AppendLine("------------------------------------------------------------");
            builder.AppendLine(output);
            if (!string.IsNullOrWhiteSpace(error))
            {
                builder.AppendLine();
                builder.AppendLine("[STDERR]");
                builder.AppendLine("------------------------------------------------------------");
                builder.AppendLine(error);
            }
            string text = builder.ToString();
            if (text == lastLogText) return;
            lastLogText = text;
            logBox.Text = text;
            logBox.SelectionStart = logBox.TextLength;
            logBox.ScrollToCaret();
        }

        private static string ReadTail(string path, int maxBytes)
        {
            try
            {
                if (!File.Exists(path)) return "暂无日志。";
                using (FileStream fs = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete))
                {
                    long length = fs.Length;
                    long start = Math.Max(0, length - maxBytes);
                    fs.Seek(start, SeekOrigin.Begin);
                    byte[] buffer = new byte[(int)(length - start)];
                    int read = fs.Read(buffer, 0, buffer.Length);
                    string text = Encoding.UTF8.GetString(buffer, 0, read);
                    if (start > 0)
                    {
                        int firstLine = text.IndexOf('\n');
                        if (firstLine >= 0) text = text.Substring(firstLine + 1);
                    }
                    return text;
                }
            }
            catch (Exception ex)
            {
                return "读取日志失败：" + ex.Message;
            }
        }

        private void TakeControlFromSupervisor()
        {
            RunHidden("schtasks.exe", "/End /TN \"LiveCompanion-Supervisor\"", 5000);
            Thread.Sleep(350);
            foreach (Process process in Process.GetProcessesByName("livecompanion-supervisor"))
            {
                try { process.Kill(); }
                catch { }
            }
            WriteLauncherLog("supervisor paused while launcher is active");
        }

        private void RestoreSupervisor()
        {
            RunHidden("schtasks.exe", "/Run /TN \"LiveCompanion-Supervisor\"", 5000);
            WriteLauncherLog("supervisor scheduled task restored");
        }

        private static int RunHidden(string file, string args, int waitMs)
        {
            try
            {
                ProcessStartInfo psi = new ProcessStartInfo(file, args);
                psi.UseShellExecute = false;
                psi.CreateNoWindow = true;
                psi.WindowStyle = ProcessWindowStyle.Hidden;
                psi.RedirectStandardOutput = true;
                psi.RedirectStandardError = true;
                using (Process process = Process.Start(psi))
                {
                    if (waitMs > 0) process.WaitForExit(waitMs);
                    return process.HasExited ? process.ExitCode : 0;
                }
            }
            catch
            {
                return -1;
            }
        }

        private static string Quote(string value)
        {
            return "\"" + (value ?? "") + "\"";
        }

        private void MinimizeToTray()
        {
            Hide();
            trayIcon.Visible = true;
            trayIcon.BalloonTipTitle = "小蓝直播搭子";
            trayIcon.BalloonTipText = "启动器仍在系统托盘运行，服务不会退出。";
            trayIcon.ShowBalloonTip(1800);
        }

        private void RestoreFromTray()
        {
            Show();
            WindowState = FormWindowState.Normal;
            Activate();
        }

        private void OnFormClosing(object sender, FormClosingEventArgs e)
        {
            if (!allowClose && e.CloseReason != CloseReason.WindowsShutDown)
            {
                e.Cancel = true;
                MinimizeToTray();
            }
        }

        private void ExitLauncher()
        {
            DialogResult result = MessageBox.Show(
                this,
                "退出启动器后是否继续保持服务运行？\r\n\r\n"
                + "“是”：退出界面并恢复后台 Supervisor 守护。\r\n"
                + "“否”：停止全部服务后退出。\r\n"
                + "“取消”：继续留在启动器。",
                "退出启动器",
                MessageBoxButtons.YesNoCancel,
                MessageBoxIcon.Question
            );
            if (result == DialogResult.Cancel) return;

            if (result == DialogResult.Yes)
            {
                RestoreSupervisor();
            }
            else
            {
                foreach (ServiceDefinition service in services.AsEnumerable().Reverse())
                    StopService(service);
            }

            allowClose = true;
            trayIcon.Visible = false;
            trayIcon.Dispose();
            uiTimer.Stop();
            Close();
            Application.Exit();
        }

        private void WriteLauncherLog(string message)
        {
            try
            {
                string path = Path.Combine(root, "data", "logs", "launcher.log");
                Directory.CreateDirectory(Path.GetDirectoryName(path));
                File.AppendAllText(
                    path,
                    DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss.fff") + " " + message + Environment.NewLine,
                    Encoding.UTF8
                );
            }
            catch { }
        }
    }

    internal static class Program
    {
        [STAThread]
        private static void Main()
        {
            bool created;
            using (Mutex mutex = new Mutex(true, "LiveCompanionLauncher.SingleInstance", out created))
            {
                if (!created)
                {
                    MessageBox.Show("小蓝直播搭子启动器已经在运行，请到系统托盘打开。", "服务启动器");
                    return;
                }
                Application.EnableVisualStyles();
                Application.SetCompatibleTextRenderingDefault(false);
                Application.Run(new MainForm());
            }
        }
    }
}
