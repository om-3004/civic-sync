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

  /// Executes the full sign-in flow:
  ///   Google OAuth → ID token → POST /auth/login → role-based navigation.
  Future<void> _signIn() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      // Force fresh sign-in by signing out of Firebase first.
      // Do NOT sign out of GoogleSignIn here — it causes authenticate() to
      // throw a canceled exception on some devices.
      await FirebaseAuth.instance.signOut();
      // Step 1: Trigger the Google account picker.
      // GoogleSignIn 7.x uses a singleton with authenticate().
      // Throws GoogleSignInException on failure; the code field distinguishes
      // cancellation from other errors.
      late final GoogleSignInAccount account;
      try {
        account = await GoogleSignIn.instance.authenticate();
      } on GoogleSignInException catch (e) {
        // Req 1.2: User cancelled — silent return, no error shown.
        if (e.code == GoogleSignInExceptionCode.canceled) {
          if (mounted) setState(() => _isLoading = false);
          return;
        }
        // Any other OAuth failure — show error and stay on sign-in screen.
        if (mounted) {
          setState(() {
            _isLoading = false;
            _errorMessage = 'Authentication failed. Please try again.';
          });
        }
        return;
      }

      // Step 2: Extract the ID token from the signed-in account.
      // authentication is a synchronous getter in google_sign_in 7.x.
      final String? idToken = account.authentication.idToken;

      if (idToken == null) {
        if (mounted) {
          setState(() {
            _errorMessage = 'Authentication failed. Please try again.';
          });
        }
        return;
      }

      // Step 2b: Sign into Firebase Auth with the Google credential so that
      // Firestore security rules (request.auth != null) are satisfied.
      final googleCredential = GoogleAuthProvider.credential(
        idToken: idToken,
      );
      final userCredential = await FirebaseAuth.instance.signInWithCredential(googleCredential);

      // Step 3: Use the Firebase ID token (not the Google Sign-In token) so
      // the UID stored in Firestore matches FirebaseAuth.instance.currentUser.uid.
      final String? firebaseIdToken = await userCredential.user?.getIdToken();
      if (firebaseIdToken == null) {
        if (mounted) {
          setState(() {
            _errorMessage = 'Authentication failed. Please try again.';
          });
        }
        return;
      }

      // POST to backend with the Firebase ID token.
      // Req 1.3: token is transmitted to backend before granting access.
      final http.Response response = await http.post(
        Uri.parse('$_backendBaseUrl/auth/login'),
        headers: {
          'Authorization': 'Bearer $firebaseIdToken',
        },
      );

      if (!mounted) return;

      if (response.statusCode == 200) {
        // Step 4: Parse role and navigate accordingly.
        final Map<String, dynamic> body =
            jsonDecode(response.body) as Map<String, dynamic>;
        final String role = (body['role'] as String?) ?? 'citizen';

        if (role == 'official') {
          Navigator.pushReplacementNamed(context, '/official-dash');
        } else {
          Navigator.pushReplacementNamed(context, '/citizen-feed');
        }
      } else if (response.statusCode == 401) {
        // Req 1.2: Backend rejected the token — show error, stay on sign-in.
        setState(() {
          _errorMessage =
              'Sign-in failed: invalid or expired credentials.';
        });
      } else {
        setState(() {
          _errorMessage = 'Sign-in failed. Please try again.';
        });
      }
    } catch (e) {
      // Network error or any unexpected exception.
      if (mounted) {
        setState(() {
          _errorMessage = 'Sign-in failed: $e';
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
                    style: const TextStyle(
                      color: Colors.red,
                      fontSize: 14,
                    ),
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
