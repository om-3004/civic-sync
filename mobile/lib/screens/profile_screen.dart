// Profile screen — user info, City Official Access PIN upgrade, and sign-out.
//
// Requirements: 6.1, 6.2, 6.3, 6.6, 6.7, 6.8

import 'dart:async';
import 'dart:convert';

import 'package:cloud_firestore/cloud_firestore.dart';
import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

const String _backendBaseUrl = String.fromEnvironment(
  'BACKEND_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

enum _PinResult { success, wrongPin, rateLimited, alreadyOfficial, error }

class _PinSubmitResult {
  final _PinResult result;
  final String? errorMessage;
  const _PinSubmitResult(this.result, {this.errorMessage});
}

class ProfileScreen extends StatefulWidget {
  const ProfileScreen({super.key});

  @override
  State<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends State<ProfileScreen> {
  User? _user;
  String? _role;
  bool _isLoadingRole = true;
  bool _navigatingToOfficial = false;
  StreamSubscription<DocumentSnapshot<Map<String, dynamic>>>? _roleSubscription;

  @override
  void initState() {
    super.initState();
    _user = FirebaseAuth.instance.currentUser;
    _subscribeToRole();
  }

  @override
  void dispose() {
    _roleSubscription?.cancel();
    super.dispose();
  }

  void _subscribeToRole() {
    final uid = _user?.uid;
    if (uid == null) return;

    _roleSubscription = FirebaseFirestore.instance
        .collection('users')
        .doc(uid)
        .snapshots()
        .listen(
      (snapshot) {
        if (!mounted) return;
        final data = snapshot.data();
        final role = (data?['role'] as String?) ?? 'citizen';
        setState(() {
          _role = role;
          _isLoadingRole = false;
        });
        if (role == 'official' && !_navigatingToOfficial) {
          _navigatingToOfficial = true;
          Future.delayed(const Duration(seconds: 1), () {
            if (mounted) {
              Navigator.pushReplacementNamed(context, '/official-dash');
            }
          });
        }
      },
      onError: (_) {
        if (!mounted) return;
        setState(() => _isLoadingRole = false);
      },
    );
  }

  Future<void> _showPinDialog() async {
    if (_role == 'official') {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Your account is already upgraded to City Official.'),
          duration: Duration(seconds: 4),
        ),
      );
      return;
    }

    _roleSubscription?.pause();
    _PinSubmitResult? submitResult;

    await _PinDialog.show(
      context: context,
      onSubmit: _callUpgradeApi,
      onResult: (r) => submitResult = r,
    );

    if (_roleSubscription?.isPaused ?? false) {
      _roleSubscription?.resume();
    }

    if (!mounted) return;
    final result = submitResult;
    if (result == null) return;

    switch (result.result) {
      case _PinResult.success:
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Upgrade successful! Redirecting to the Official Dashboard…'),
            duration: Duration(seconds: 3),
          ),
        );
      case _PinResult.rateLimited:
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Too many failed attempts. Please try again in 15 minutes.'),
            backgroundColor: Colors.red,
            duration: Duration(seconds: 6),
          ),
        );
      case _PinResult.alreadyOfficial:
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Your account is already upgraded to City Official.'),
            duration: Duration(seconds: 4),
          ),
        );
        setState(() => _role = 'official');
      default:
        break;
    }
  }

  Future<_PinSubmitResult> _callUpgradeApi(String pin) async {
    try {
      final idToken = await FirebaseAuth.instance.currentUser?.getIdToken();
      final response = await http.post(
        Uri.parse('$_backendBaseUrl/auth/upgrade'),
        headers: {
          'Content-Type': 'application/json',
          if (idToken != null) 'Authorization': 'Bearer $idToken',
        },
        body: jsonEncode({'pin': pin}),
      );
      switch (response.statusCode) {
        case 200:
          return const _PinSubmitResult(_PinResult.success);
        case 403:
          return const _PinSubmitResult(_PinResult.wrongPin,
              errorMessage: 'Incorrect PIN. Please try again.');
        case 429:
          return const _PinSubmitResult(_PinResult.rateLimited);
        case 409:
          return const _PinSubmitResult(_PinResult.alreadyOfficial);
        default:
          return _PinSubmitResult(_PinResult.error,
              errorMessage: 'Upgrade failed (${response.statusCode}). Please try again.');
      }
    } catch (_) {
      return const _PinSubmitResult(_PinResult.error,
          errorMessage: 'Network error. Please check your connection.');
    }
  }

  Future<void> _signOut() async {
    await FirebaseAuth.instance.signOut();
    if (mounted) {
      Navigator.pushReplacementNamed(context, '/auth');
    }
  }

  @override
  Widget build(BuildContext context) {
    final user = _user;
    final displayName = user?.displayName ?? 'User';
    final email = user?.email ?? '';
    final photoUrl = user?.photoURL;
    final isOfficial = _role == 'official';

    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      appBar: AppBar(
        title: const Text('Profile'),
        backgroundColor: const Color(0xFF1A73E8),
        foregroundColor: Colors.white,
        elevation: 0,
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.center,
          children: [
            const SizedBox(height: 16),
            CircleAvatar(
              radius: 48,
              backgroundColor: const Color(0xFFE8F0FE),
              backgroundImage: (photoUrl != null && photoUrl.isNotEmpty)
                  ? NetworkImage(photoUrl)
                  : null,
              child: (photoUrl == null || photoUrl.isEmpty)
                  ? Text(
                      displayName.isNotEmpty ? displayName[0].toUpperCase() : '?',
                      style: const TextStyle(
                        fontSize: 36,
                        fontWeight: FontWeight.bold,
                        color: Color(0xFF1A73E8),
                      ),
                    )
                  : null,
            ),
            const SizedBox(height: 16),
            Text(
              displayName,
              style: const TextStyle(
                fontSize: 22,
                fontWeight: FontWeight.w700,
                color: Color(0xFF202124),
              ),
            ),
            const SizedBox(height: 4),
            if (email.isNotEmpty)
              Text(
                email,
                style: const TextStyle(fontSize: 14, color: Color(0xFF5F6368)),
              ),
            const SizedBox(height: 8),
            if (!_isLoadingRole)
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
                decoration: BoxDecoration(
                  color: isOfficial ? Colors.green.shade50 : const Color(0xFFE8F0FE),
                  borderRadius: BorderRadius.circular(20),
                  border: Border.all(
                    color: isOfficial
                        ? Colors.green.shade300
                        : const Color(0xFF1A73E8).withValues(alpha: 0.4),
                  ),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      isOfficial ? Icons.verified_outlined : Icons.person_outline,
                      size: 14,
                      color: isOfficial ? Colors.green.shade700 : const Color(0xFF1A73E8),
                    ),
                    const SizedBox(width: 6),
                    Text(
                      isOfficial ? 'City Official' : 'Citizen',
                      style: TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                        color: isOfficial ? Colors.green.shade700 : const Color(0xFF1A73E8),
                      ),
                    ),
                  ],
                ),
              ),
            const SizedBox(height: 32),
            const Divider(),
            const SizedBox(height: 24),
            _OfficialAccessSection(
              isLoadingRole: _isLoadingRole,
              isOfficial: isOfficial,
              onUpgradeTap: _showPinDialog,
            ),
            const SizedBox(height: 32),
            const Divider(),
            const SizedBox(height: 16),
            SizedBox(
              width: double.infinity,
              child: OutlinedButton.icon(
                onPressed: _signOut,
                icon: const Icon(Icons.logout),
                label: const Text('Sign Out'),
                style: OutlinedButton.styleFrom(
                  foregroundColor: Colors.red.shade700,
                  side: BorderSide(color: Colors.red.shade300),
                  padding: const EdgeInsets.symmetric(vertical: 14),
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(10),
                  ),
                ),
              ),
            ),
            const SizedBox(height: 24),
          ],
        ),
      ),
    );
  }
}

// ── City Official Access section ──────────────────────────────────────────────

class _OfficialAccessSection extends StatelessWidget {
  final bool isLoadingRole;
  final bool isOfficial;
  final VoidCallback onUpgradeTap;

  const _OfficialAccessSection({
    required this.isLoadingRole,
    required this.isOfficial,
    required this.onUpgradeTap,
  });

  @override
  Widget build(BuildContext context) {
    if (isLoadingRole) {
      return const Center(
        child: SizedBox(
          width: 24,
          height: 24,
          child: CircularProgressIndicator(strokeWidth: 2, color: Color(0xFF1A73E8)),
        ),
      );
    }

    if (isOfficial) {
      return Container(
        width: double.infinity,
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: Colors.green.shade50,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: Colors.green.shade200),
        ),
        child: Row(
          children: [
            Icon(Icons.verified, color: Colors.green.shade700, size: 28),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('Account Upgraded',
                      style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w700,
                          color: Colors.green.shade800)),
                  const SizedBox(height: 2),
                  Text('You already have City Official access.',
                      style: TextStyle(fontSize: 13, color: Colors.green.shade700)),
                ],
              ),
            ),
          ],
        ),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('City Official Access',
            style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: Color(0xFF202124))),
        const SizedBox(height: 6),
        const Text(
          'Are you a government official? Enter your secret PIN to unlock the official management dashboard.',
          style: TextStyle(fontSize: 13, color: Color(0xFF5F6368)),
        ),
        const SizedBox(height: 14),
        SizedBox(
          width: double.infinity,
          child: ElevatedButton.icon(
            onPressed: onUpgradeTap,
            icon: const Icon(Icons.admin_panel_settings_outlined),
            label: const Text('City Official Access'),
            style: ElevatedButton.styleFrom(
              backgroundColor: const Color(0xFF0D47A1),
              foregroundColor: Colors.white,
              padding: const EdgeInsets.symmetric(vertical: 14),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
            ),
          ),
        ),
      ],
    );
  }
}

// ── PIN dialog ────────────────────────────────────────────────────────────────

class _PinDialog extends StatefulWidget {
  final Future<_PinSubmitResult> Function(String pin) onSubmit;
  final void Function(_PinSubmitResult) onResult;

  const _PinDialog({required this.onSubmit, required this.onResult});

  static Future<void> show({
    required BuildContext context,
    required Future<_PinSubmitResult> Function(String pin) onSubmit,
    required void Function(_PinSubmitResult) onResult,
  }) {
    return showDialog<void>(
      context: context,
      barrierDismissible: true,
      builder: (_) => _PinDialog(onSubmit: onSubmit, onResult: onResult),
    );
  }

  @override
  State<_PinDialog> createState() => _PinDialogState();
}

class _PinDialogState extends State<_PinDialog> {
  final _controller = TextEditingController();
  final _focusNode = FocusNode();
  String? _error;
  bool _isSubmitting = false;

  @override
  void dispose() {
    _focusNode.unfocus();
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_controller.text.trim().isEmpty) {
      setState(() => _error = 'PIN cannot be empty.');
      return;
    }
    setState(() {
      _isSubmitting = true;
      _error = null;
    });

    final result = await widget.onSubmit(_controller.text);
    if (!mounted) return;

    if (result.result == _PinResult.success ||
        result.result == _PinResult.rateLimited ||
        result.result == _PinResult.alreadyOfficial) {
      widget.onResult(result);
      _focusNode.unfocus();
      Navigator.of(context).pop();
    } else {
      setState(() {
        _isSubmitting = false;
        _error = result.errorMessage;
      });
    }
  }

  void _cancel() {
    _focusNode.unfocus();
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('City Official Access'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Enter the secret PIN to upgrade your account to City Official status.',
            style: TextStyle(fontSize: 14, color: Color(0xFF5F6368)),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _controller,
            focusNode: _focusNode,
            autofocus: true,
            obscureText: true,
            textInputAction: TextInputAction.done,
            decoration: InputDecoration(
              labelText: 'PIN',
              hintText: 'Enter PIN',
              errorText: _error,
              border: const OutlineInputBorder(),
              prefixIcon: const Icon(Icons.lock_outline),
            ),
            onChanged: (_) {
              if (_error != null) setState(() => _error = null);
            },
            onSubmitted: (_) {
              if (!_isSubmitting) _submit();
            },
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: _isSubmitting ? null : _cancel,
          child: const Text('Cancel'),
        ),
        ElevatedButton(
          onPressed: _isSubmitting ? null : _submit,
          style: ElevatedButton.styleFrom(
            backgroundColor: const Color(0xFF1A73E8),
            foregroundColor: Colors.white,
          ),
          child: _isSubmitting
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                )
              : const Text('Submit'),
        ),
      ],
    );
  }
}
