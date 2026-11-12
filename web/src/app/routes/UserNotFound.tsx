import { useNavigate } from 'react-router';
import { NotFoundState } from '../../shared';

/** The 404 page rendered under the user and admin route tables. */
export function UserNotFound() {
  const navigate = useNavigate();

  return (
    <NotFoundState
      testId="shell-notfound"
      onHome={() => {
        void navigate('/');
      }}
      onBack={() => {
        void navigate(-1);
      }}
    />
  );
}
