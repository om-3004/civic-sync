// Auth screen — Google OAuth sign-in with backend token verification.
//
// Sign-in flow:
//   1. GoogleSignIn.instance.authenticate() — triggers the Google account
//      picker (google_sign_in 7.x singleton API).
//   2. Extract idToken from account.authentication (synchronous getter).
//   3. POST /auth/login with Authorization: Bearer <idToken>.
//   4. Navigate to /citizen-feed or /official-dash based on the role field.
//
// Requirements: 1.1, 1.2, 1.3

import 'dart:convert';

import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';
import 'package:google_sign_in/google_sign_in.dart';
import 'package:http/http.dart' as http;

// Backend base URL — override at build time via --dart-define=BACKEND_URL=...
// Defaults to the Android emulator loopback to the host machine.
const String _backendBaseUrl = String.fromEnvironment(
  'BACKEND_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

/// Full Google OAuth authentication screen.
///
/// Manages loading state and error messages while coordinating the
/// Google sign-in flow and backend login call.
class AuthScreen extends StatefulWidget {
  const AuthScreen({super.key});

  @override
  State<AuthScreen> createState() => _AuthScreenState();
}

class _AuthScreenState extends State<AuthScreen> {
  bool _isLoading = false;
  String? _errorMessage;
  String _debugStep = ''; // temporary debug indicator

  /// Executes the full sign-in flow:
  ///   Google OAuth → ID token → POST /auth/login → role-based navigation.
  Future<void> _signIn() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      setState(() => _debugStep = 'step1: authenticating...');
      // Step 1: Trigger the Google account picker.
      late final GoogleSignInAccount account;
      try {
        account = await GoogleSignIn.instance.authenticate();
      } on GoogleSignInException catch (e) {
        if (e.code == GoogleSignInExceptionCode.canceled) {
          if (mounted) setState(() { _isLoading = false; _debugStep = ''; });
          return;
        }
        if (mounted) {
          setState(() {
            _isLoading = false;
            _debugStep = '';
            _errorMessage = 'Authentication failed: ${e.code}';
          });
        }
        return;
      }

      if (mounted) setState(() => _debugStep = 'step2: extracting idToken...');
      final String? idToken = account.authentication.idToken;

      if (idToken == null) {
        if (mounted) setState(() { _errorMessage = 'Sign-in failed: idToken was null.'; _debugStep = ''; });
        return;
      }

      if (mounted) setState(() => _debugStep = 'step3: signing into Firebase...');
      final googleCredential = GoogleAuthProvider.credential(idToken: idToken);
      final userCredential = await FirebaseAuth.instance.signInWithCredential(googleCredential);

      if (mounted) setState(() => _debugStep = 'step4: getting Firebase token...');
      final String? firebaseIdToken = await userCredential.user?.getIdToken();
      if (firebaseIdToken == null) {
        if (mounted) setState(() { _errorMessage = 'Sign-in failed: firebaseIdToken was null.'; _debugStep = ''; });
        return;
      }

      if (mounted) setState(() => _debugStep = 'step5: calling backend $_backendBaseUrl ...');
      final http.Response response = await http.post(
        Uri.parse('$_backendBaseUrl/auth/login'),
        headers: {'Authorization': 'Bearer $firebaseIdToken'},
      );

      if (mounted) setState(() => _debugStep = 'step6: got ${response.statusCode}');

      if (!mounted) return;

      if (response.statusCode == 200) {
        setState(() => _debugStep = '');
        final Map<String, dynamic> body = jsonDecode(response.body) as Map<String, dynamic>;
        final String role = (body['role'] as String?) ?? 'citizen';
        if (role == 'official') {
          Navigator.pushReplacementNamed(context, '/official-dash');
        } else {
          Navigator.pushReplacementNamed(context, '/citizen-feed');
        }
      } else {
        setState(() {
          _debugStep = '';
          _errorMessage = 'Backend error ${response.statusCode}: ${response.body}';
        });
      }
    } catch (e) {
      // Network error or any unexpected exception.
      if (mounted) {
        setState(() {
          _errorMessage = 'Sign-in failed: ${e.runtimeType}: $e';
        });
      }
    } finally {
      if (mounted) {
        setState(() {
          _isLoading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 32.0),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                // App logo / wordmark
                const Icon(
                  Icons.location_city,
                  size: 72,
                  color: Color(0xFF1A73E8),
                ),
                const SizedBox(height: 24),
                Text(
                  'CivicSync',
                  style: Theme.of(context).textTheme.headlineLarge?.copyWith(
                        fontWeight: FontWeight.bold,
                        color: const Color(0xFF202124),
                      ),
                ),
                const SizedBox(height: 8),
                Text(
                  'Report civic issues in your community',
                  textAlign: TextAlign.center,
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        color: const Color(0xFF5F6368),
                      ),
                ),
                const SizedBox(height: 48),

                // Sign-in button or loading indicator.
                if (_isLoading)
                  const CircularProgressIndicator(
                    color: Color(0xFF1A73E8),
                  )
                else
                  ElevatedButton.icon(
                    onPressed: _signIn,
                    icon: const Icon(Icons.login),
                    label: const Text('Sign in with Google'),
                    style: ElevatedButton.styleFrom(
                      minimumSize: const Size.fromHeight(50),
                      backgroundColor: const Color(0xFF1A73E8),
                      foregroundColor: Colors.white,
                    ),
                  ),

                // Error message (Req 1.2: shown on OAuth failure or 401).
                if (_errorMessage != null && _errorMessage!.isNotEmpty) ...[
                  const SizedBox(height: 16),
                  Text(
                    _errorMessage!,
                    textAlign: TextAlign.center,
                    style: const TextStyle(color: Colors.red, fontSize: 14),
                  ),
                ],
                if (_debugStep.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    _debugStep,
                    textAlign: TextAlign.center,
                    style: const TextStyle(color: Colors.grey, fontSize: 12),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
