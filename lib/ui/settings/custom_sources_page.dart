import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core_bridge.dart';
import '../../local_store.dart';
import '../../models.dart';
import '../app_scope.dart';
import 'settings_rows.dart';

/// 自定义源管理：录入苹果CMS 采集站点，运行时注册成站源。
class CustomSourcesPage extends StatefulWidget {
  const CustomSourcesPage({super.key, required this.store});
  final LocalStore store;

  @override
  State<CustomSourcesPage> createState() => _CustomSourcesPageState();
}

class _CustomSourcesPageState extends State<CustomSourcesPage> {
  List<CustomSourceSite> _sites = const [];
  bool _loading = true;

  AppRepository get _repository => AppScope.of(context).repository;
  bool get _admin => widget.store.profile.admin;

  @override
  void initState() {
    super.initState();
    _reload();
  }

  Future<void> _reload() async {
    try {
      final sites = await _repository.customSources();
      if (!mounted) return;
      setState(() {
        _sites = sites;
        _loading = false;
      });
    } catch (failure) {
      if (!mounted) return;
      setState(() => _loading = false);
      _notify(failure.toString());
    }
  }

  void _notify(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _add() async {
    final draft = await showDialog<_CustomSourceDraft>(
      context: context,
      builder: (context) => _CustomSourceDialog(repository: _repository),
    );
    if (draft == null || !mounted) return;
    try {
      await _repository.addCustomSource(draft.name, draft.base, api: draft.api);
      widget.store.refreshSources();
      await _reload();
      _notify('已添加 ${draft.name}');
    } catch (failure) {
      _notify(failure.toString());
    }
  }

  Future<void> _edit(CustomSourceSite site) async {
    final draft = await showDialog<_CustomSourceDraft>(
      context: context,
      builder: (context) =>
          _CustomSourceDialog(repository: _repository, site: site),
    );
    if (draft == null || !mounted) return;
    try {
      await _repository.updateCustomSource(
        site.id,
        name: draft.name,
        base: draft.base,
        api: draft.api,
      );
      widget.store.refreshSources();
      await _reload();
      _notify('已保存 ${draft.name}');
    } catch (failure) {
      _notify(failure.toString());
    }
  }

  Future<void> _remove(CustomSourceSite site) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除自定义源'),
        content: Text('确定删除「${site.name}」吗？该源的剧集记录会保留但无法继续播放。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await _repository.removeCustomSource(site.id);
      widget.store.refreshSources();
      await _reload();
      _notify('已删除 ${site.name}');
    } catch (failure) {
      _notify(failure.toString());
    }
  }

  Future<void> _import() async {
    final content = await showDialog<String>(
      context: context,
      builder: (context) => const _ImportDialog(),
    );
    if (content == null || !mounted) return;
    try {
      final result = await _repository.importCustomSources(
        await _resolveImport(content),
      );
      widget.store.refreshSources();
      await _reload();
      final added = (result['added'] as List? ?? const []).length;
      final updated = (result['updated'] as List? ?? const []).length;
      final failed = (result['failed'] as List? ?? const [])
          .whereType<String>()
          .toList();
      final message = failed.isEmpty
          ? '导入完成：新增 $added，更新 $updated'
          : '导入完成：新增 $added，更新 $updated，失败 ${failed.length}（${failed.first}）';
      _notify(message);
    } catch (failure) {
      _notify(failure.toString());
    }
  }

  /// 导入内容如果是一行远程配置地址，先抓取该地址的文本。
  Future<String> _resolveImport(String content) async {
    final text = content.trim();
    if (!text.startsWith('http') || text.contains('\n')) return text;
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 10);
    try {
      final request = await client.getUrl(Uri.parse(text));
      final response = await request.close();
      if (response.statusCode != HttpStatus.ok) {
        throw AppFailure('远程配置返回 ${response.statusCode}');
      }
      return await response.transform(utf8.decoder).join();
    } finally {
      client.close(force: true);
    }
  }

  Future<void> _export() async {
    try {
      final content = await _repository.exportCustomSources();
      await Clipboard.setData(ClipboardData(text: content));
      _notify('已复制自定义源配置到剪贴板');
    } catch (failure) {
      _notify(failure.toString());
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('自定义源'),
        actions: [
          if (_admin) ...[
            IconButton(
              tooltip: '导入',
              onPressed: _import,
              icon: const Icon(Icons.file_download_outlined),
            ),
            IconButton(
              tooltip: '导出',
              onPressed: _export,
              icon: const Icon(Icons.file_upload_outlined),
            ),
            IconButton(
              tooltip: '添加',
              onPressed: _add,
              icon: const Icon(Icons.add),
            ),
          ],
        ],
      ),
      floatingActionButton: _admin
          ? FloatingActionButton.extended(
              onPressed: _add,
              icon: const Icon(Icons.add),
              label: const Text('添加站点'),
            )
          : null,
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 96),
              children: [
                if (!_admin)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(
                      '需要管理员才能管理自定义源',
                      style: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                if (_sites.isEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 40),
                    child: Column(
                      children: [
                        Icon(
                          Icons.dns_outlined,
                          size: 44,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                        const SizedBox(height: 12),
                        Text('还没有自定义源', style: theme.textTheme.titleMedium),
                        const SizedBox(height: 8),
                        Text(
                          '添加苹果CMS采集站（支持 /api.php/provide/vod 接口），\n也可以导入 TVBox 配置里的站点。',
                          textAlign: TextAlign.center,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                            height: 1.6,
                          ),
                        ),
                      ],
                    ),
                  )
                else
                  SettingsCard(
                    children: [
                      for (final site in _sites)
                        ListTile(
                          minTileHeight: 72,
                          title: Text(site.name),
                          subtitle: Text(
                            site.api.isEmpty
                                ? site.base
                                : '${site.base}\n${site.api}',
                          ),
                          isThreeLine: site.api.isNotEmpty,
                          trailing: _admin
                              ? Row(
                                  mainAxisSize: MainAxisSize.min,
                                  children: [
                                    IconButton(
                                      tooltip: '编辑',
                                      onPressed: () => _edit(site),
                                      icon: const Icon(Icons.edit_outlined),
                                    ),
                                    IconButton(
                                      tooltip: '删除',
                                      onPressed: () => _remove(site),
                                      icon: const Icon(Icons.delete_outline),
                                    ),
                                  ],
                                )
                              : null,
                        ),
                    ],
                  ),
                const SizedBox(height: 16),
                Text(
                  '自定义源与内置采集站使用同一套苹果CMS解析：分类、列表、搜索、详情与多线路取流。'
                  '收藏、追剧、下载等记录按源隔离，关闭该源后不显示。',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    height: 1.6,
                  ),
                ),
              ],
            ),
    );
  }
}

class _CustomSourceDraft {
  const _CustomSourceDraft(this.name, this.base, this.api);
  final String name;
  final String base;
  final String api;
}

class _CustomSourceDialog extends StatefulWidget {
  const _CustomSourceDialog({required this.repository, this.site});
  final AppRepository repository;
  final CustomSourceSite? site;

  @override
  State<_CustomSourceDialog> createState() => _CustomSourceDialogState();
}

class _CustomSourceDialogState extends State<_CustomSourceDialog> {
  late final TextEditingController _name;
  late final TextEditingController _base;
  late final TextEditingController _api;
  bool _testing = false;
  String _testResult = '';

  @override
  void initState() {
    super.initState();
    _name = TextEditingController(text: widget.site?.name ?? '');
    _base = TextEditingController(text: widget.site?.base ?? '');
    _api = TextEditingController(text: widget.site?.api ?? '');
  }

  @override
  void dispose() {
    _name.dispose();
    _base.dispose();
    _api.dispose();
    super.dispose();
  }

  Future<void> _test() async {
    final target = _api.text.trim().isEmpty
        ? _base.text.trim()
        : _api.text.trim();
    if (target.isEmpty) {
      setState(() => _testResult = '请先填写站点地址');
      return;
    }
    setState(() {
      _testing = true;
      _testResult = '';
    });
    try {
      final result = await widget.repository.analyzeCustomSource(target);
      if (!mounted) return;
      final categories = (result['categories'] as List? ?? const []).length;
      setState(() {
        _testing = false;
        _testResult = '接口可用：${result['api']}（$categories 个分类）';
        if (_name.text.trim().isEmpty) {
          _name.text = result['name'] as String? ?? '';
        }
      });
    } catch (failure) {
      if (!mounted) return;
      setState(() {
        _testing = false;
        _testResult = failure.toString();
      });
    }
  }

  void _submit() {
    final base = _base.text.trim();
    if (base.isEmpty) {
      setState(() => _testResult = '请填写站点地址');
      return;
    }
    Navigator.pop(
      context,
      _CustomSourceDraft(_name.text.trim(), base, _api.text.trim()),
    );
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.site == null ? '添加自定义源' : '编辑自定义源'),
    content: SizedBox(
      width: 460,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _base,
              autofocus: widget.site == null,
              decoration: const InputDecoration(
                labelText: '站点地址',
                hintText: 'https://www.example.com',
                helperText: '填站点首页即可，会自动探测采集接口',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _name,
              decoration: const InputDecoration(
                labelText: '名称',
                hintText: '留空则使用域名',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _api,
              decoration: const InputDecoration(
                labelText: '接口地址（可选）',
                hintText: 'https://www.example.com/api.php/provide/vod',
                helperText: '探测不到时手动填写采集接口',
              ),
            ),
            if (_testResult.isNotEmpty) ...[
              const SizedBox(height: 12),
              Align(
                alignment: Alignment.centerLeft,
                child: Text(
                  _testResult,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: _testing ? null : _test,
        child: _testing
            ? const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Text('测试'),
      ),
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(onPressed: _submit, child: const Text('保存')),
    ],
  );
}

class _ImportDialog extends StatefulWidget {
  const _ImportDialog();

  @override
  State<_ImportDialog> createState() => _ImportDialogState();
}

class _ImportDialogState extends State<_ImportDialog> {
  final _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('导入自定义源'),
    content: SizedBox(
      width: 520,
      child: TextField(
        controller: _controller,
        maxLines: 12,
        minLines: 6,
        decoration: const InputDecoration(
          hintText: '支持本应用导出的 JSON、TVBox/CatVod 配置、\n每行一个「名称,网址」，或一行远程配置地址',
          border: OutlineInputBorder(),
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        onPressed: () => Navigator.pop(context, _controller.text),
        child: const Text('导入'),
      ),
    ],
  );
}
